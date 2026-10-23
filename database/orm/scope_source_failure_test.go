package orm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Both public APIs must retain a failed lazy declaration across terminals.
// The driver's statement log catches a later query silently losing its scopes.
func TestScopeSourceFailureNeverReachesSQL(t *testing.T) {
	for _, old := range []bool{false, true} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(fmt.Sprintf("old=%t/%s", old, mode), func(t *testing.T) {
				calls := 0
				fail := func() {
					calls++
					if mode == "panic" {
						panic("private tenant value")
					}
					runtime.Goexit()
				}
				var statements []string
				db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("stored", nil, nil)})
				ctx := t.Context()
				var all, count, cursor, chunk, remove func() error
				if old {
					id := query.NewScalarField[hookedRow, int64]("hooked", "id", codec.Signed[int64]())
					d := query.Define("hooked", "id", []query.Column{{Name: "id"}}, func(database.Row) (hookedRow, error) { return hookedRow{}, nil },
						query.NewModelField("id", codec.Signed[int64](), func(m hookedRow) int64 { return m.ID }))
					q := query.ForModel(d.WithGlobalScopeSource(func() []query.GlobalScope[hookedRow] {
						fail()
						return []query.GlobalScope[hookedRow]{query.NewGlobalScope("tenant", id.Eq(7))}
					}))
					all = func() error { _, err := q.All(ctx, db); return err }
					count = func() error { _, err := q.Count(ctx, db); return err }
					cursor = func() error { _, err := q.CursorPaginate(ctx, db, query.CursorRequest[hookedRow]{Size: 2}); return err }
					chunk = func() error { return q.Chunk(ctx, db, 2, func([]hookedRow) error { return nil }) }
					remove = func() error { _, err := q.Where(id.Eq(1)).Remove(ctx, db); return err }
				} else {
					table := hookedFields(nil, WithGlobalScopes(func(table hookedTable) []GlobalScope[hookedRow] {
						fail()
						return []GlobalScope[hookedRow]{Scope(table.Table, "tenant", func(context.Context) (Predicate[hookedRow], error) { return table.Key().Eq(7), nil })}
					}))
					q := table.Query()
					all = func() error { _, err := q.All(ctx, db); return err }
					count = func() error { _, err := q.Count(ctx, db); return err }
					cursor = func() error {
						_, err := CursorPaginate(ctx, db, q, query.CursorRequest[hookedRow]{Size: 2})
						return err
					}
					chunk = func() error { return q.Chunk(ctx, db, 2, func([]hookedRow) error { return nil }) }
					remove = func() error { _, err := table.Delete(ctx, db, 1); return err }
				}
				for _, terminal := range []struct {
					name string
					run  func() error
				}{{"first", all}, {"later", all}, {"count", count}, {"cursor", cursor}, {"chunk", chunk}, {"delete", remove}} {
					err := terminal.run()
					if !errors.Is(err, fault.Panicked) {
						t.Fatalf("%s did not retain source failure: %v", terminal.name, err)
					}
					if strings.Contains(fmt.Sprintf("%s %v %+v %#v", err.Error(), err, err, err), "private tenant value") {
						t.Fatal("source failure disclosed panic value")
					}
					if len(statements) != 0 {
						t.Fatalf("%s reached SQL after source failure: %v", terminal.name, statements)
					}
				}
				if calls != 1 {
					t.Fatal("source was retried", calls)
				}
			})
		}
	}
}
