package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type nativePrivateError struct {
	calls atomic.Int32
	mode  string
}

func (e *nativePrivateError) Error() string {
	e.calls.Add(1)
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	if e.mode == "panic" {
		panic("private")
	}
	return "private native SQL"
}
func (e *nativePrivateError) Format(s fmt.State, _ rune) {
	e.calls.Add(1)
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	if e.mode == "panic" {
		panic("private")
	}
	_, _ = s.Write([]byte("private native SQL"))
}
func (e *nativePrivateError) Is(error) bool {
	if e.mode == "is_exit" {
		runtime.Goexit()
	}
	if e.mode == "is_panic" {
		panic("private")
	}
	return false
}
func (e *nativePrivateError) As(any) bool {
	if e.mode == "as_exit" {
		runtime.Goexit()
	}
	if e.mode == "as_panic" {
		panic("private")
	}
	return false
}
func (e *nativePrivateError) Unwrap() error {
	if e.mode == "cycle" {
		return e
	}
	return nil
}

type failingText struct{ cause error }

func (v failingText) TextValue() (pgtype.Text, error) { return pgtype.Text{}, v.cause }
func (v failingText) Format(s fmt.State, _ rune)      { panic("private argument formatted") }

type failingInteger struct{ cause error }

func (v failingInteger) Int64Value() (pgtype.Int8, error) { return pgtype.Int8{}, v.cause }

type failingNativeScanner struct{ cause error }

func (v *failingNativeScanner) ScanText(pgtype.Text) error  { return v.cause }
func (v *failingNativeScanner) ScanInt64(pgtype.Int8) error { return v.cause }

type failingSQLValuer struct{ cause error }

func (v failingSQLValuer) Value() (driver.Value, error) { return nil, v.cause }
func revealNative(err error) error {
	var result error
	contained := callback.Isolated("reveal native SQL test error", func() error { result = safeerror.Reveal("native SQL failed", err); return nil })
	if contained != nil {
		return contained
	}
	return result
}
func assertNativeCause(t *testing.T, err error, cause *nativePrivateError) {
	t.Helper()
	if err == nil {
		t.Fatal("expected conversion failure")
	}
	revealed := revealNative(err)
	if !errors.Is(revealed, cause) {
		t.Fatal("native cause identity lost")
	}
	var typed *nativePrivateError
	if !errors.As(revealed, &typed) || typed != cause {
		t.Fatal("native concrete cause lost")
	}
	_ = fmt.Sprintf("%v %+v %#v", revealed, revealed, revealed)
	if cause.calls.Load() != 0 {
		t.Fatal("native error formatter invoked")
	}
}

func TestPostgresNativeCallbacksContainErrorsBeforeDriverInspection(t *testing.T) {
	for _, mode := range []string{"private", "panic", "goexit", "is_panic", "is_exit", "as_panic", "as_exit", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			cause := &nativePrivateError{mode: mode}
			m := pgtype.NewMap()
			protectCodecMap(m)
			composite := &pgtype.Type{Name: "test_composite", OID: 80001, Codec: &pgtype.CompositeCodec{Fields: []pgtype.CompositeCodecField{{Name: "value", Type: mustPGType(m, pgtype.TextOID)}}}}
			protectCodecType(m, composite)
			pointer := failingText{cause}
			indirect := &pointer
			cases := []struct {
				name  string
				oid   uint32
				value any
			}{
				{"text", pgtype.TextOID, failingText{cause}},
				{"integer", pgtype.Int8OID, failingInteger{cause}},
				{"pointer", pgtype.TextOID, &indirect},
				{"valuer", pgtype.TextOID, failingSQLValuer{cause}},
				{"array", pgtype.TextArrayOID, []failingText{{cause}}},
				{"composite", composite.OID, pgtype.CompositeFields{failingText{cause}}},
				{"range", pgtype.Int8rangeOID, pgtype.Range[failingInteger]{Lower: failingInteger{cause}, LowerType: pgtype.Inclusive, UpperType: pgtype.Unbounded, Valid: true}},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					for _, format := range []int16{pgtype.TextFormatCode, pgtype.BinaryFormatCode} {
						_, err := m.Encode(test.oid, format, protectValue(m, test.value), nil)
						assertNativeCause(t, err, cause)
						if errors.Is(err, driver.ErrBadConn) {
							t.Fatal("conversion acquired driver retry")
						}
						var retry interface{ SafeToRetry() bool }
						if errors.As(err, &retry) {
							t.Fatal("application As escaped opaque boundary")
						}
					}
				})
			}
			for _, oid := range []uint32{pgtype.TextOID, pgtype.Int8OID} {
				destination := &failingNativeScanner{cause}
				err := m.Scan(oid, pgtype.TextFormatCode, []byte("42"), protectTarget(destination))
				assertNativeCause(t, err, cause)
			}
		})
	}
}
func mustPGType(m *pgtype.Map, oid uint32) *pgtype.Type { typ, _ := m.TypeForOID(oid); return typ }
func TestPostgresProtectedPlansRetainNativeValuesAndTypeChanges(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	plain := pgtype.NewMap()
	values := []struct {
		oid   uint32
		value any
	}{
		{pgtype.Int8OID, int64(42)}, {pgtype.Int8OID, int16(7)}, {pgtype.TextOID, "ok"}, {pgtype.TextArrayOID, []string{"a", "b"}}, {pgtype.Int8ArrayOID, []int64{1, 2}}, {pgtype.JSONBOID, map[string]any{"a": 1}}, {pgtype.JSONBOID, map[string]any(nil)},
		{0, int64(42)}, {0, "ok"}, {0, []int64{1, 2}},
	}
	for _, test := range values {
		for _, format := range []int16{pgtype.TextFormatCode, pgtype.BinaryFormatCode} {
			want, werr := plain.Encode(test.oid, format, test.value, nil)
			got, gerr := m.Encode(test.oid, format, protectValue(m, test.value), nil)
			if (gerr == nil) != (werr == nil) || string(got) != string(want) || (got == nil) != (want == nil) {
				t.Fatalf("protected encoding differs for %T oid %d format %d", test.value, test.oid, format)
			}
		}
	}
	var strings []string
	if err := m.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, []byte("{one,two}"), protectTarget(&strings)); err != nil || len(strings) != 2 || strings[1] != "two" {
		t.Fatal("native array scan changed")
	}
}
func TestPostgresNativeCallbackExitIsContained(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	_, err := m.Encode(pgtype.TextOID, pgtype.TextFormatCode, protectValue(m, exitingNativeText{}), nil)
	revealed := revealNative(err)
	found := false
	contained := callback.Isolated("inspect native exit", func() error { found = errorgraph.Is(revealed, fault.Panicked); return nil })
	if contained != nil || !found {
		t.Fatal("native Goexit lost contained panic classification")
	}
}

type exitingNativeText struct{}

func (exitingNativeText) TextValue() (pgtype.Text, error) {
	runtime.Goexit()
	return pgtype.Text{}, nil
}

type hostileQueryRewriter struct {
	cause    error
	expected *pgx.Conn
	called   *bool
}

func (r hostileQueryRewriter) RewriteQuery(_ context.Context, conn *pgx.Conn, _ string, _ []any) (string, []any, error) {
	if conn != r.expected {
		panic("rewriter connection changed")
	}
	*r.called = true
	return "", nil, r.cause
}
func TestPostgresQueryRewriterErrorsStayOpaque(t *testing.T) {
	for _, mode := range []string{"private", "panic", "goexit", "is_panic", "is_exit", "as_panic", "as_exit", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			cause := &nativePrivateError{mode: mode}
			conn := &pgx.Conn{}
			called := false
			m := pgtype.NewMap()
			protectCodecMap(m)
			r := protectedRewriter{source: hostileQueryRewriter{cause: cause, expected: conn, called: &called}, types: m}
			_, _, err := r.RewriteQuery(t.Context(), conn, "SELECT 1", nil)
			if !called {
				t.Fatal("rewriter not called")
			}
			assertNativeCause(t, err, cause)
			if errors.Is(err, driver.ErrBadConn) {
				t.Fatal("rewriter caused retry")
			}
			var target interface{ SafeToRetry() bool }
			if errors.As(err, &target) {
				t.Fatal("rewriter cause escaped pgx")
			}
		})
	}
}
func TestPostgresUninferredNativeCompositeCallbacks(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	typ := &pgtype.Type{Name: "custom_composite", OID: 80002, Codec: &pgtype.CompositeCodec{Fields: []pgtype.CompositeCodecField{{Name: "value", Type: mustPGType(m, pgtype.TextOID)}}}}
	m.RegisterType(typ)
	cause := &nativePrivateError{}
	_, err := m.Encode(typ.OID, pgtype.BinaryFormatCode, protectValue(m, pgtype.CompositeFields{failingText{cause}}), nil)
	assertNativeCause(t, err, cause)
	dest := pgtype.CompositeFields{&failingNativeScanner{cause}}
	bytes, err := m.Encode(typ.OID, pgtype.BinaryFormatCode, protectValue(m, pgtype.CompositeFields{"ok"}), nil)
	if err != nil {
		t.Fatal("custom composite success changed")
	}
	assertNativeCause(t, m.Scan(typ.OID, pgtype.BinaryFormatCode, bytes, protectTarget(dest)), cause)
}
