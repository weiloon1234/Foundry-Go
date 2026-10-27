package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateSQLError struct{ formatted atomic.Int32 }

func (e *privateSQLError) Error() string { e.formatted.Add(1); return "private SQL error" }
func (e *privateSQLError) Format(s fmt.State, _ rune) {
	e.formatted.Add(1)
	fmt.Fprint(s, "private SQL formatter")
}

type errorValuer struct{ cause error }

func (v errorValuer) Value() (driver.Value, error) { return nil, v.cause }

type errorScanner struct{ cause error }

func (s errorScanner) Scan(any) error { return s.cause }

func assertSQLCause(t *testing.T, err error, private *privateSQLError) {
	t.Helper()
	var retained *privateSQLError
	if !errors.Is(err, private) || !errors.As(err, &retained) || retained != private {
		t.Fatal("SQL callback cause lost")
	}
	_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	if private.formatted.Load() != 0 {
		t.Fatal("SQL callback error formatted before redaction")
	}
}

func TestRawSQLCallbacksRedactBeforeStandardLibraryFormatting(t *testing.T) {
	for _, operation := range []string{"exec", "query", "scan", "codec_scan", "validator_scan"} {
		t.Run(operation, func(t *testing.T) {
			private := &privateSQLError{}
			state := &driverState{}
			db := open(t, state, nil)
			switch operation {
			case "exec", "query":
				for _, arg := range []any{errorValuer{private}, sql.Named("key", errorValuer{private})} {
					args := []any{arg}
					var err error
					if operation == "exec" {
						_, err = db.Exec(t.Context(), "SELECT $1", args...)
					} else {
						_, err = db.Query(t.Context(), "SELECT $1", args...)
					}
					assertSQLCause(t, err, private)
					if args[0] != arg {
						t.Fatal("argument slice mutated")
					}
					if errors.Is(err, fault.Invalid) {
						t.Fatal("infrastructure failure became invalid input")
					}
				}
			default:
				rows, err := db.Query(t.Context(), "SELECT 42")
				if err != nil || !rows.Next() {
					t.Fatal("missing row", err)
				}
				var destination any = errorScanner{private}
				var decoded int64 = 9
				if operation == "codec_scan" {
					c := codec.New(func(v int64) (driver.Value, error) { return v, nil }, func(any) (int64, error) { return 0, private })
					destination = c.Scan(&decoded)
				}
				if operation == "validator_scan" {
					destination = codec.Signed[int64]().Validated(func(int64) error { return private }).Scan(&decoded)
				}
				assertSQLCause(t, rows.Scan(destination), private)
				assertSQLCause(t, rows.Err(), private)
				if state.rowsClosed.Load() != 1 || decoded != 9 {
					t.Fatal("scan failure leaked rows or published partial decoding")
				}
			}
			if _, err := db.Exec(t.Context(), "SELECT 1"); err != nil {
				t.Fatal("callback failure leaked connection", err)
			}
		})
	}
}

func TestSQLValuerNilAndNamedArgumentSemantics(t *testing.T) {
	var nilValue *errorValuer
	state := &driverState{exec: func(_ context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
		if len(args) != 2 || args[0].Value != nil || args[1].Name != "key" || args[1].Value != nil {
			t.Fatal("valuer adaptation changed NULL or named arguments")
		}
		return driver.RowsAffected(1), nil
	}}
	db := open(t, state, nil)
	if _, err := db.Exec(t.Context(), "SELECT $1,$2", nilValue, sql.Named("key", errorValuer{})); err != nil {
		t.Fatal(err)
	}
}

func TestSQLCallbackClassificationKeepsContextPrecedence(t *testing.T) {
	private := &privateSQLError{}
	db := open(t, &driverState{}, nil)
	cause := errors.Join(private, context.Canceled, context.DeadlineExceeded)
	_, err := db.Exec(t.Context(), "SELECT $1", errorValuer{cause})
	assertSQLCause(t, err, private)
	var classified *database.Error
	if !errors.As(err, &classified) || classified.Code() != database.DeadlineExceeded {
		t.Fatal("context classification precedence changed")
	}
}

type hostileSQLInspectionError struct {
	privateSQLError
	mode string
}

func (e *hostileSQLInspectionError) Error() string {
	e.formatted.Add(1)
	switch e.mode {
	case "format_panic":
		panic("private")
	case "format_goexit":
		runtime.Goexit()
	}
	return "private SQL error"
}
func (e *hostileSQLInspectionError) Format(s fmt.State, _ rune) {
	e.formatted.Add(1)
	switch e.mode {
	case "format_panic":
		panic("private")
	case "format_goexit":
		runtime.Goexit()
	}
	_, _ = s.Write([]byte("private SQL error"))
}
func (e *hostileSQLInspectionError) Is(error) bool {
	switch e.mode {
	case "is_panic":
		panic("private")
	case "is_goexit":
		runtime.Goexit()
	}
	return false
}
func (e *hostileSQLInspectionError) As(any) bool {
	switch e.mode {
	case "as_panic":
		panic("private")
	case "as_goexit":
		runtime.Goexit()
	}
	return false
}
func (e *hostileSQLInspectionError) Unwrap() error {
	if e.mode == "cycle" {
		return e
	}
	return nil
}
func TestSQLCallbacksKeepHostileInspectionOutsideForeignLibraries(t *testing.T) {
	for _, mode := range []string{"format_panic", "format_goexit", "is_panic", "is_goexit", "as_panic", "as_goexit", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			for _, operation := range []string{"valuer", "scanner", "driver"} {
				t.Run(operation, func(t *testing.T) {
					cause := &hostileSQLInspectionError{mode: mode}
					state := &driverState{}
					if operation == "driver" {
						state.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, cause }
					}
					db := open(t, state, nil)
					var err error
					switch operation {
					case "valuer":
						v := errorValuer{cause}
						p := &v
						_, err = db.Exec(t.Context(), "SELECT $1", &p)
					case "driver":
						_, err = db.Exec(t.Context(), "SELECT 1")
					case "scanner":
						rows, e := db.Query(t.Context(), "SELECT 1")
						if e != nil || !rows.Next() {
							t.Fatal("query failed")
						}
						s := errorScanner{cause}
						p := &s
						err = rows.Scan(&p)
					}
					if err == nil || !errors.Is(err, cause) {
						t.Fatal("hostile SQL cause lost")
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
					if cause.formatted.Load() != 0 {
						t.Fatal("private error formatted")
					}
				})
			}
		})
	}
}
