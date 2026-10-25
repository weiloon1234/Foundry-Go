package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type callbackPrivateError struct{ calls atomic.Int32 }

func (e *callbackPrivateError) Error() string { e.calls.Add(1); return "private PostgreSQL callback" }
func (e *callbackPrivateError) Format(s fmt.State, _ rune) {
	e.calls.Add(1)
	_, _ = s.Write([]byte("private PostgreSQL callback"))
}

type callbackText struct{ cause error }

func (v callbackText) TextValue() (pgtype.Text, error) { return pgtype.Text{}, v.cause }

type callbackTextScanner struct{ cause error }

func (v *callbackTextScanner) ScanText(pgtype.Text) error { return v.cause }

type callbackRewrite struct{ cause error }

func (v callbackRewrite) RewriteQuery(context.Context, *pgx.Conn, string, []any) (string, []any, error) {
	return "", nil, v.cause
}
func TestPostgresNativeCallbackFailureReleasesConnection(t *testing.T) {
	db := pgtest.Open(t)
	for _, operation := range []string{"exec", "array", "query", "scan", "rewriter"} {
		t.Run(operation, func(t *testing.T) {
			cause := &callbackPrivateError{}
			var err error
			switch operation {
			case "exec":
				_, err = db.Exec(t.Context(), "SELECT $1::text", callbackText{cause})
			case "array":
				_, err = db.Exec(t.Context(), "SELECT $1::text[]", []callbackText{{cause}})
			case "query":
				_, err = db.Query(t.Context(), "SELECT $1::text", callbackText{cause})
			case "scan":
				err = database.ScanOne(t.Context(), db, "SELECT 'value'::text", nil, &callbackTextScanner{cause})
			case "rewriter":
				_, err = db.Exec(t.Context(), "SELECT 1", callbackRewrite{cause})
			}
			if err == nil || !errors.Is(err, cause) {
				t.Fatal("callback cause lost")
			}
			var retained *callbackPrivateError
			if !errors.As(err, &retained) || retained != cause {
				t.Fatal("callback concrete identity lost")
			}
			_ = fmt.Sprintf("%v %+v %#v", err, err, err)
			if cause.calls.Load() != 0 {
				t.Fatal("private callback formatted")
			}
			var answer int64
			if err := database.ScanOne(t.Context(), db, "SELECT 42::bigint", nil, &answer); err != nil || answer != 42 {
				t.Fatal("callback failure retained connection")
			}
		})
	}
}

type registeredCatchAll struct {
	oid    uint32
	cause  error
	fail   bool
	calls  *atomic.Int32
	native **pgx.Conn
}

func (r registeredCatchAll) RewriteQuery(_ context.Context, conn *pgx.Conn, query string, args []any) (string, []any, error) {
	if r.native != nil {
		*r.native = conn
	}
	conn.TypeMap().RegisterType(&pgtype.Type{Name: "foundry_h1_catchall", OID: r.oid, Codec: &pgtype.JSONCodec{
		Marshal:   func(any) ([]byte, error) { return nil, r.cause },
		Unmarshal: func([]byte, any) error { return r.cause },
	}})
	if r.calls != nil {
		r.calls.Add(1)
	}
	if r.fail {
		return "", nil, r.cause
	}
	return query, args, nil
}
func TestPostgresRegisteredCatchAllCallbacksRemainContained(t *testing.T) {
	setup := pgtest.Open(t)
	schema := pgtest.Namespace(t, setup)
	typeName := schema + ".h1_callback_type"
	if _, err := setup.Exec(t.Context(), `CREATE TYPE "`+schema+`".h1_callback_type AS ENUM ('value')`); err != nil {
		t.Fatal("create isolated callback type failed")
	}
	cause := &callbackPrivateError{}
	var registrations atomic.Int32
	db := pgtest.Open(t, func(config *postgres.Config) {
		config.Pool.MaxOpen = 1
		config.Pool.MaxIdle = 1
		config.RegisterTypes = func(ctx context.Context, registry *postgres.TypeRegistry) error {
			typ, err := registry.LoadType(ctx, typeName)
			if err != nil {
				return err
			}
			typ.Codec = &pgtype.JSONCodec{Marshal: func(any) ([]byte, error) { return nil, cause }, Unmarshal: func([]byte, any) error { return cause }}
			if err := registry.RegisterType(typ); err != nil {
				return err
			}
			registrations.Add(1)
			return nil
		}
	})
	_, err := db.Exec(t.Context(), "SELECT $1::"+typeName, struct{ Value string }{"value"})
	if err == nil || !errors.Is(err, cause) {
		t.Fatal("registered catch-all encoding cause lost")
	}
	var destination struct{ Value string }
	err = database.ScanOne(t.Context(), db, "SELECT 'value'::"+typeName, nil, &destination)
	if err == nil || !errors.Is(err, cause) {
		t.Fatal("registered catch-all decoding cause lost")
	}
	if registrations.Load() != 1 {
		t.Fatal("registered codec unexpectedly discarded or recreated connection")
	}
	if cause.calls.Load() != 0 {
		t.Fatal("registered callback error formatted")
	}
	var answer int64
	if err := database.ScanOne(t.Context(), db, "SELECT 42::bigint", nil, &answer); err != nil || answer != 42 {
		t.Fatal("registered callback failure retained connection")
	}
}

func TestPostgresRewriteRegistryMutationFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			db := pgtest.Open(t, func(config *postgres.Config) { config.Pool.MaxOpen = 1; config.Pool.MaxIdle = 1 })
			cause := &callbackPrivateError{}
			var calls atomic.Int32
			var rejected *pgx.Conn
			_, err := db.Exec(t.Context(), "SELECT 1", registeredCatchAll{oid: pgtype.TextOID, cause: cause, fail: fail, calls: &calls, native: &rejected})
			if err == nil || calls.Load() != 1 || rejected == nil || !rejected.IsClosed() {
				t.Fatal("registry mutation was accepted, retried, or returned to the pool")
			}
			if fail {
				if !errors.Is(err, cause) {
					t.Fatal("failed mutator cause lost")
				}
			} else {
				var migration *fault.Error
				if !errors.As(err, &migration) || !strings.Contains(migration.Message(), "postgres.Config.RegisterTypes") {
					t.Fatal("registry rejection omitted registration migration path")
				}
			}
			var replacement *pgx.Conn
			var answer int64
			if err := database.ScanOne(t.Context(), db, rewriteBoundarySQL, []any{rewriteBoundaryProbe{native: &replacement}, int64(42)}, &answer); err != nil || answer != 42 || replacement == nil || replacement == rejected {
				t.Fatal("rejected registry connection was not safely replaced")
			}
			if cause.calls.Load() != 0 {
				t.Fatal("registry rejection formatted private cause")
			}
		})
	}
}

func TestPostgresTypeRegistrationHookFailuresAreContained(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			config := pgtest.Config(t)
			config.Pool.MaxOpen = 1
			config.Pool.MaxIdle = 1
			cause := &callbackPrivateError{}
			var calls atomic.Int32
			config.RegisterTypes = func(context.Context, *postgres.TypeRegistry) error {
				calls.Add(1)
				switch mode {
				case "error":
					return cause
				case "panic":
					panic("private registration panic")
				case "goexit":
					runtime.Goexit()
				}
				return nil
			}
			db, err := postgres.Open(t.Context(), config)
			if db != nil {
				_ = db.Close(t.Context())
				t.Fatal("failed registration published a database")
			}
			if err == nil || calls.Load() != 1 {
				t.Fatal("failed registration succeeded or retried")
			}
			if mode == "error" {
				var retained *callbackPrivateError
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("registration callback lost original cause")
				}
			} else if !errors.Is(err, fault.Panicked) {
				t.Fatal("registration callback exit was not contained")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.calls.Load() != 0 {
				t.Fatal("registration failure formatted private callback content")
			}
			healthy := pgtest.Open(t)
			var answer int64
			if err := database.ScanOne(t.Context(), healthy, "SELECT 42::bigint", nil, &answer); err != nil || answer != 42 {
				t.Fatal("healthy pool failed after registration failure")
			}
		})
	}
}

func TestPostgresTypeRegistryClosesAfterSuccessfulHook(t *testing.T) {
	var escaped *postgres.TypeRegistry
	var calls atomic.Int32
	db := pgtest.Open(t, func(config *postgres.Config) {
		config.Pool.MaxOpen = 1
		config.Pool.MaxIdle = 1
		config.RegisterTypes = func(_ context.Context, registry *postgres.TypeRegistry) error {
			escaped = registry
			calls.Add(1)
			return nil
		}
	})
	if escaped == nil || calls.Load() != 1 {
		t.Fatal("physical connection did not run registration hook")
	}
	if err := escaped.RegisterType(&pgtype.Type{Name: "text", OID: pgtype.TextOID, Codec: pgtype.TextCodec{}}); err == nil {
		t.Fatal("escaped registry registered a type")
	}
	if err := escaped.RegisterDefaultPgType("", "text"); err == nil {
		t.Fatal("escaped registry changed default type mapping")
	}
	if typ, err := escaped.LoadType(t.Context(), "pg_catalog.int8"); err == nil || typ != nil {
		t.Fatal("escaped registry loaded a server type")
	}
	var answer int64
	if err := database.ScanOne(t.Context(), db, rewriteBoundarySQL, []any{rewriteBoundaryProbe{}, int64(42)}, &answer); err != nil || answer != 42 || calls.Load() != 1 {
		t.Fatal("closed registry changed the live connection")
	}
}
