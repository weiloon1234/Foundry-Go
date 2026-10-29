package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRetrievalFactoriesAndCallbacksRunAfterCompleteClosedBatch(t *testing.T) {
	var trace []string
	closed := 0
	var expected database.Executor
	factory := func(label string) func() RetrievalHooks[hookedRow] {
		return func() RetrievalHooks[hookedRow] {
			trace = append(trace, label+".factory")
			if closed != 1 {
				t.Error("factory ran before row closure")
			}
			return RetrievalHooks[hookedRow]{Retrieved: func(ctx context.Context, ex database.Executor, m hookedRow) error {
				if closed != 1 || ex != expected || m.Name == "" || len(m.Body) == 0 {
					t.Error("callback lost closed rows, executor or complete model")
				}
				trace = append(trace, fmt.Sprintf("%s.%d", label, m.ID))
				return nil
			}}
		}
	}
	q := hookedFields(nil, WithRetrievalHooks(factory("local")))
	declaration, err := NewRetrievalObserver[hookedRow]("read.audit").Declare(factory("observer"))
	if err != nil {
		t.Fatal(err)
	}
	values := append(hookValues("first", nil, nil), hookValues("second", nil, nil)...)
	values[1][0] = int64(2)
	db := hookDB(t, testConnector{columns: hookColumns, values: values, onClose: func() { closed++ }}, declaration)
	wrapper := catchObservers(db, db.FoundryObservers)
	expected = wrapper
	items, err := All(t.Context(), wrapper, q.Query())
	if err != nil || len(items) != 2 || wrapper.calls != 0 {
		t.Fatal(err, len(items), wrapper.calls)
	}
	want := []string{"local.factory", "observer.factory", "local.1", "observer.1", "local.2", "observer.2"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatal(trace)
	}
}

func TestRetrievalSkipsEmptyFailedAndWriteBatches(t *testing.T) {
	for _, mode := range []string{"empty", "decode", "close", "write", "scalar"} {
		t.Run(mode, func(t *testing.T) {
			factories := 0
			q := hookedFields(nil, WithRetrievalHooks(func() RetrievalHooks[hookedRow] { factories++; return RetrievalHooks[hookedRow]{} }))
			c := testConnector{columns: hookColumns, values: hookValues("stored", nil, nil)}
			switch mode {
			case "empty":
				c.values = nil
			case "decode":
				c.values[0][0] = "invalid integer"
			case "close":
				c.closeErr = errors.New("close failed")
			case "scalar":
				c.columns = []string{"count"}
				c.values = [][]driver.Value{{int64(1)}}
			}
			db := hookDB(t, c)
			var err error
			switch mode {
			case "write":
				_, err = Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), InputTextSet(q.Name, "stored"), ColumnSet(q.Body, []byte{1}))
			case "scalar":
				_, err = Count(t.Context(), db, q.Query())
			default:
				_, err = All(t.Context(), db, q.Query())
			}
			if (mode == "decode" || mode == "close") != (err != nil) || factories != 0 {
				t.Fatal(err, factories)
			}
		})
	}
}

func TestRetrievalRawAndObserverOnlyStreaming(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			q := hookedFields(nil)
			factories, calls, closed := 0, 0, 0
			declaration, err := NewRetrievalObserver[hookedRow]("read.only").Declare(func() RetrievalHooks[hookedRow] {
				factories++
				return RetrievalHooks[hookedRow]{Retrieved: func(context.Context, database.Executor, hookedRow) error {
					calls++
					if closed == 0 {
						t.Error("retrieval ran with open rows")
					}
					return nil
				}}
			})
			if err != nil {
				t.Fatal(err)
			}
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("stored", nil, nil), onClose: func() { closed++ }}, declaration)
			if raw {
				_, err = Raw(t.Context(), db, q.Table, `SELECT * FROM hooked`)
			} else {
				err = Each(t.Context(), db, q.Query(), func(hookedRow) error {
					if closed != 1 || calls != 1 {
						t.Error("stream delivered before retrieval/closure")
					}
					return nil
				})
				if !strings.Contains(statements[0], "LIMIT") {
					t.Fatal("retrieval streamed open rows")
				}
			}
			if err != nil || factories != 1 || calls != 1 {
				t.Fatal(err, factories, calls)
			}
		})
	}
}

func TestRetrievalFailureRedactionIsolationAndReuse(t *testing.T) {
	secret := errors.New("private stored model")
	for _, mode := range []string{"error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			q := hookedFields(nil, WithRetrievalHooks(func() RetrievalHooks[hookedRow] {
				return RetrievalHooks[hookedRow]{Retrieved: func(context.Context, database.Executor, hookedRow) error {
					switch mode {
					case "panic":
						panic("private stored model")
					case "goexit":
						runtime.Goexit()
					case "cancel":
						cancel()
					}
					return secret
				}}
			}))
			db := hookDB(t, testConnector{respond: func(sql string) ([]string, [][]driver.Value) {
				if strings.Contains(sql, "COUNT") {
					return []string{"count"}, [][]driver.Value{{int64(1)}}
				}
				return hookColumns, hookValues("stored", nil, nil)
			}})
			items, err := All(ctx, db, q.Query())
			if err == nil || items != nil {
				t.Fatal("failed callback published results", err)
			}
			if mode == "error" && !errors.Is(err, secret) {
				t.Fatal("lost cause", err)
			}
			for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				if strings.Contains(text, "private stored model") {
					t.Fatal("retrieval diagnostic leak")
				}
			}
			// Scope failure releases ownership and leaves the same pool usable.
			if _, err = Count(t.Context(), db, q.Query()); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := Check(hookedFields(nil, WithRetrievalHooks[hookedRow](nil))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil factory accepted", err)
	}
	var empty RetrievalObserver[hookedRow]
	if empty.Validate() == nil {
		t.Fatal("zero observer accepted")
	}
}
