package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type scopeFormattingError struct {
	method, mode string
	calls        int
}

func (e *scopeFormattingError) invoke(method string) {
	e.calls++
	if e.method != method {
		return
	}
	switch e.mode {
	case "panic":
		panic("private scope failure")
	case "goexit":
		runtime.Goexit()
	}
}
func (e *scopeFormattingError) Error() string { e.invoke("Error"); return "private scope error" }
func (e *scopeFormattingError) Format(s fmt.State, _ rune) {
	e.invoke("Format")
	_, _ = s.Write([]byte("private scope format"))
}

func TestScopeCallbacksKeepIdentityWithoutFormatting(t *testing.T) {
	for _, owner := range []string{"transaction", "savepoint", "session", "observer"} {
		for _, method := range []string{"Error", "Format"} {
			for _, mode := range []string{"private", "panic", "goexit"} {
				t.Run(owner+"/"+method+"/"+mode, func(t *testing.T) {
					state := &driverState{}
					db := open(t, state, oneObserverConnection)
					cause := &scopeFormattingError{method: method, mode: mode}
					var err error
					switch owner {
					case "transaction":
						err = db.Transaction(t.Context(), func(*database.Tx) error { return cause })
					case "savepoint":
						err = db.Transaction(t.Context(), func(tx *database.Tx) error {
							return tx.Savepoint(t.Context(), func(*database.Tx) error { return cause })
						})
					case "session":
						err = db.Session(t.Context(), func(*database.Session) error { return cause })
					case "observer":
						rows, openErr := db.Query(t.Context(), "select 42")
						if openErr != nil {
							t.Fatal("could not open observer rows")
						}
						err = rows.WithObserverScope(t.Context(), func(context.Context) error { return cause })
						if state.rowsClosed.Load() != 1 {
							t.Fatal("observer failure retained rows")
						}
						if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") {
							t.Fatal("observer scope exposed private cause")
						}
					}
					var original *scopeFormattingError
					if !errors.Is(err, cause) || !errors.As(err, &original) || original != cause {
						t.Fatal("callback error identity lost")
					}
					if owner == "transaction" && err != cause {
						t.Fatal("transaction callback lost exact identity")
					}
					if cause.calls != 0 {
						t.Fatal("scope invoked application formatting")
					}
					if state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
						t.Fatal("scope failure committed or retained ownership")
					}
					if (owner == "transaction" || owner == "savepoint") && state.rolledBack.Load() != 1 {
						t.Fatal("callback failure did not roll back")
					}
				})
			}
		}
	}
}

func TestModelDecodersContainFailuresWithAndWithoutRetrievalHooks(t *testing.T) {
	for _, hooks := range []bool{false, true} {
		for _, mode := range []string{"Error-private", "Error-panic", "Error-goexit", "Format-private", "Format-panic", "Format-goexit", "callback-panic", "callback-goexit"} {
			t.Run(fmt.Sprintf("hooks=%t/%s", hooks, mode), func(t *testing.T) {
				state := retrievalFixtureDriver([][]driver.Value{{int64(1), "first"}, {int64(2), "second"}})
				db := open(t, state, oneObserverConnection)
				parts := strings.Split(mode, "-")
				cause := &scopeFormattingError{method: parts[0], mode: parts[1]}
				decoded, observed := 0, 0
				decode := func(raw any) (string, error) {
					decoded++
					if decoded == 1 {
						return raw.(string), nil
					}
					if parts[0] == "callback" {
						if parts[1] == "goexit" {
							runtime.Goexit()
						}
						panic("private decoder callback")
					}
					return "", cause
				}
				c := codec.New(func(s string) (driver.Value, error) { return s, nil }, decode).WithParameterType(codec.TypeText)
				var options []orm.Option

				if hooks {
					options = append(options, orm.WithRetrievalHooks(func() orm.RetrievalHooks[retrievalFixtureRecord] {
						return orm.RetrievalHooks[retrievalFixtureRecord]{Retrieved: func(context.Context, database.Executor, retrievalFixtureRecord) error { observed++; return nil }}
					}))
				}
				definition := orm.Define("retrieved_records", func(b orm.Builder[retrievalFixtureRecord], m *retrievalFixtureRecord) retrievalFixtureTable {
					return retrievalFixtureTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Codec(c))}
				}, options...)
				items, err := orm.All(t.Context(), db, definition.Query())
				if err == nil || items != nil || decoded != 2 || observed != 0 {
					t.Fatal("decoder failure returned partial rows or ran hooks")
				}
				if parts[0] == "callback" {
					if !errors.Is(err, fault.Panicked) {
						t.Fatal("decoder callback failure was not contained")
					}
				} else {
					var original *scopeFormattingError
					if !errors.Is(err, cause) || !errors.As(err, &original) || original != cause {
						t.Fatal("decoder cause identity lost")
					}
				}
				if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") || cause.calls != 0 {
					t.Fatal("decoder failure exposed private cause")
				}
				if state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("decoder failure retained stream ownership")
				}
				if _, err := db.Exec(t.Context(), "healthy after decoder failure"); err != nil {
					t.Fatal("pool unusable after decoder failure")
				}
			})
		}
	}
}

func TestPatchEachDraftFailuresKeepCauseAndRollBack(t *testing.T) {
	for _, method := range []string{"Error", "Format"} {
		for _, mode := range []string{"private", "panic", "goexit"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				state := mutationDriver([][]driver.Value{{int64(42), "stored"}})
				db := open(t, state, oneObserverConnection)
				cause := &scopeFormattingError{method: method, mode: mode}
				calls := 0
				result, err := orm.UpdateEach(t.Context(), db, mutationModel(), 1, func(context.Context, *database.Tx, mutationRecord) ([]orm.Assignment[mutationRecord], error) {
					calls++
					return []orm.Assignment[mutationRecord]{}, cause
				})
				var original *scopeFormattingError
				if len(result) != 0 || calls != 1 || !errors.Is(err, cause) || !errors.As(err, &original) || original != cause {
					t.Fatal("draft failure returned results or lost cause")
				}
				if cause.calls != 0 || state.rolledBack.Load() != 1 || state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("draft error formatted, committed, or retained transaction")
				}
			})
		}
	}
}
