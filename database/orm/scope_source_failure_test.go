package orm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Native reads and writes must retain a failed lazy declaration across terminals.
// The driver's statement log catches a later query silently losing its scopes.
func TestScopeSourceFailureNeverReachesSQL(t *testing.T) {
	{
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(mode, func(t *testing.T) {
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
				var all, count, cursor, chunk, remove, update, setUpdate, setDelete func() error
				table := hookedFields(nil, WithGlobalScopes(func(table hookedTable) []GlobalScope[hookedRow] {
					fail()
					return []GlobalScope[hookedRow]{Scope(table.Table, "tenant", func(context.Context) (Predicate[hookedRow], error) { return table.Key().Eq(7), nil })}
				}))
				q := table.Query()
				all = func() error { _, err := q.All(ctx, db); return err }
				count = func() error { _, err := q.Count(ctx, db); return err }
				cursor = func() error {
					_, err := CursorPaginate(ctx, db, q, CursorRequest[hookedRow]{Size: 2})
					return err
				}
				chunk = func() error { return q.Chunk(ctx, db, 2, func([]hookedRow) error { return nil }) }
				remove = func() error { _, err := table.Delete(ctx, db, 1); return err }
				update = func() error { _, err := table.Update(ctx, db, 1, table.Name.Set("changed")); return err }
				setUpdate = func() error { _, err := q.UpdateAll(ctx, db, table.Name.Set("changed")); return err }
				setDelete = func() error { _, err := q.DeleteAll(ctx, db); return err }
				for _, terminal := range []struct {
					name string
					run  func() error
				}{{"first", all}, {"later", all}, {"count", count}, {"cursor", cursor}, {"chunk", chunk}, {"delete", remove}, {"update", update}, {"set update", setUpdate}, {"set delete", setDelete}} {
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
