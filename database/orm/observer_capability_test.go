package orm

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// A consumer can infer K from a method value without importing its internal
// type. A structural assertion must never deliver an issued capability here.
type catcher[K any] struct {
	*database.DB
	calls, transactions int
	captured            K
}

func catchObservers[K any](db *database.DB, _ func(K) (lifecycle.Observers, bool)) *catcher[K] {
	return &catcher[K]{DB: db}
}

func (c *catcher[K]) FoundryObservers(k K) (lifecycle.Observers, bool) {
	c.calls++
	c.captured = k
	return lifecycle.Observers{}, true
}

func (c *catcher[K]) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	c.transactions++
	return c.DB.Transaction(ctx, fn, options...)
}

func unsealedObservers[K any](lookup func(any, K) (lifecycle.Observers, bool), writer any) (lifecycle.Observers, bool) {
	var zero K
	return lookup(writer, zero)
}

func TestObserverCatcherCannotProveAbsenceOrCaptureCapability(t *testing.T) {
	for _, registered := range []bool{false, true} {
		name := "empty"
		if registered {
			name = "observed"
		}
		t.Run(name, func(t *testing.T) {
			q := hookedFields(nil)
			observed := 0
			var declarations []lifecycle.Declaration
			if registered {
				declaration, err := NewObserver[hookedRow]("catcher.audit").Declare(func() Hooks[hookedRow] {
					return Hooks[hookedRow]{Creating: func(context.Context, *database.Tx, *Write[hookedRow]) error {
						observed++
						return nil
					}}
				})
				if err != nil {
					t.Fatal(err)
				}
				declarations = append(declarations, declaration)
			}
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("name", nil, nil)}, declarations...)
			if _, known := unsealedObservers(query.FoundryObservers, db); known {
				t.Fatal("zero capability opened the shared observer bridge")
			}
			// A concrete owner can prove absence; a wrapper always requires ack.
			_, err := q.Query().UpdateAll(t.Context(), db, q.Name.Set("name"))
			if registered && !errors.Is(err, fault.Invalid) || !registered && err != nil {
				t.Fatal("concrete owner lookup", err)
			}
			wrapper := catchObservers(db, db.FoundryObservers)
			before := len(statements)
			if _, err = q.Query().UpdateAll(t.Context(), wrapper, q.Name.Set("name")); !errors.Is(err, fault.Invalid) {
				t.Fatal("wrapper bypassed set-write acknowledgement", err)
			}
			if len(statements) != before || wrapper.transactions != 0 || wrapper.calls != 0 {
				t.Fatal("unacknowledged set write reached application code or SQL")
			}
			if _, err = WithoutModelHooks(q.Query()).UpdateAll(t.Context(), wrapper, q.Name.Set("name")); err != nil {
				t.Fatal(err)
			}
			if observed != 0 {
				t.Fatal("acknowledged set write invoked a model observer")
			}
			if _, err = q.Insert(t.Context(), wrapper, q.Key().Set(1), q.Name.Set("name"), q.Body.Set([]byte{1})); err != nil {
				t.Fatal(err)
			}
			wantObserved := 0
			if registered {
				wantObserved = 1
			}
			if wrapper.calls != 0 || wrapper.transactions != 2 || observed != wantObserved {
				t.Fatal("wrapper captured authority or hid transaction observers", wrapper.calls, wrapper.transactions, observed)
			}
			if _, known := db.FoundryObservers(wrapper.captured); known {
				t.Fatal("catcher obtained a valid observer capability")
			}
		})
	}
}

func TestFrameworkOwnerFormsPreserveObservers(t *testing.T) {
	for _, registered := range []bool{false, true} {
		name := "empty"
		if registered {
			name = "observed"
		}
		t.Run(name, func(t *testing.T) {
			q := hookedFields(nil)
			observed := 0
			var declarations []lifecycle.Declaration
			if registered {
				declaration, err := NewObserver[hookedRow]("owner.audit").Declare(func() Hooks[hookedRow] {
					return Hooks[hookedRow]{Creating: func(context.Context, *database.Tx, *Write[hookedRow]) error {
						observed++
						return nil
					}}
				})
				if err != nil {
					t.Fatal(err)
				}
				declarations = append(declarations, declaration)
			}
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("name", nil, nil)}, declarations...)
			check := func(name string, writer database.Transactor) {
				t.Helper()
				t.Run(name, func(t *testing.T) {
					set, known := ownerObservers(writer)
					if !known || lifecycle.HasObservers[hookedRow](set) != registered {
						t.Fatal("owner lost its observer snapshot", known)
					}
					before := len(statements)
					_, err := q.Query().UpdateAll(t.Context(), writer, q.Name.Set("name"))
					if registered {
						if !errors.Is(err, fault.Invalid) || len(statements) != before {
							t.Fatal("unacknowledged set write reached SQL", err)
						}
					} else if err != nil {
						t.Fatal("observer-free owner required acknowledgement", err)
					}
					beforeObserved := observed
					if _, err := q.Insert(t.Context(), writer, q.Key().Set(1), q.Name.Set("name"), q.Body.Set([]byte{1})); err != nil {
						t.Fatal(err)
					}
					want := beforeObserved
					if registered {
						want++
					}
					if observed != want {
						t.Fatalf("observer calls = %d, want %d", observed, want)
					}
				})
			}
			primary := db.Primary()
			check("pool", db)
			check("primary value", primary)
			check("primary pointer", &primary)
			if err := db.Session(t.Context(), func(session *database.Session) error {
				check("session", session)
				return session.Transaction(t.Context(), func(tx *database.Tx) error {
					check("session transaction", tx)
					return nil
				})
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
				check("transaction", tx)
				return tx.Transaction(t.Context(), func(nested *database.Tx) error {
					check("savepoint", nested)
					return nil
				})
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
