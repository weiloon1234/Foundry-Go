package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// A live callback replaces inspection of legacy Mutation/MutationValues. Input
// getters must expose the original input only at the explicit inspection boundary.
func TestWriteDraftRetentionAndExplicitInput(t *testing.T) {
	type record struct{ ID string }
	type input struct{ Text string }
	type fields struct {
		orm.Table[record, string]
		ID orm.InputKey[record, input, string]
	}
	const secret = "assignment-input-SECRET-945817"
	stop := errors.New("inspected draft")
	for _, mode := range []string{"stored", "input"} {
		t.Run(mode, func(t *testing.T) {
			var table fields
			calls := 0
			table = orm.Define("predicate_owners", func(b orm.Builder[record], m *record) fields {
				key := orm.TableOf(b, &m.ID)
				return fields{key, orm.InputKeyOf(b, key.Key(), func(v input) (string, error) { return v.Text, nil })}
			}, orm.WithHooks(func() orm.Hooks[record] {
				return orm.Hooks[record]{Creating: func(_ context.Context, _ *database.Tx, w *orm.Write[record]) error {
					calls++
					for _, v := range []any{w, *w, struct{ private *orm.Write[record] }{w}, struct{ nested any }{struct{ private *orm.Write[record] }{w}}, struct {
						nested struct{ private *orm.Write[record] }
					}{struct{ private *orm.Write[record] }{w}}} {
						for _, format := range []string{"%v", "%+v", "%#v"} {
							for _, text := range []string{fmt.Sprintf(format, v), fmt.Errorf("draft: "+format, v).Error()} {
								if strings.Contains(text, secret) {
									t.Fatal("draft disclosed pending input")
								}
							}
						}
					}
					if mode == "stored" {
						v, ok := orm.WriteGet(w, func() func(arg0 string) orm.Assignment[record] {
							receiver := table.Key()
							return func(arg0 string) orm.Assignment[record] { return orm.KeySet(receiver, arg0) }
						}()).Get()
						if !ok || v != secret {
							t.Fatal("stored assignment inspection changed")
						}
					} else {
						v, ok := orm.WriteGet(w, func() func(arg0 input) orm.Assignment[record] {
							receiver := table.ID
							return func(arg0 input) orm.Assignment[record] { return orm.InputKeySet(receiver, arg0) }
						}()).Get()
						if !ok || v.Text != secret {
							t.Fatal("mutator input inspection changed")
						}
					}
					return stop
				}}
			}))
			var sql []string
			db := hookDB(t, testConnector{statements: &sql})
			assignment := orm.KeySet(table.Key(), secret)
			if mode == "input" {
				assignment = orm.InputKeySet(table.ID, input{secret})
			}
			if _, err := orm.Insert(t.Context(), db, table.Table, assignment); !errors.Is(err, stop) || calls != 1 || len(sql) != 0 {
				t.Fatal("draft inspection did not stop before SQL", calls, err)
			}
		})
	}
}
