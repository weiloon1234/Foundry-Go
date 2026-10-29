package goway_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/value"
)

type bindingParent struct {
	ID       int64
	Children relation.Many[bindingChild]
}
type bindingChild struct {
	ID, ParentID int64
	Slug         string
}
type bindingParents struct {
	orm.Table[bindingParent, int64]
	Children orm.Many[bindingParent, bindingChild]
}
type bindingChildren struct {
	orm.Table[bindingChild, int64]
	ParentID orm.Number[bindingChild, int64]
	Slug     orm.Text[bindingChild, string]
}

func TestNestedORMBindingHasNoGlobalFallback(t *testing.T) {
	parents := orm.Define("binding_parents", func(b orm.Builder[bindingParent], m *bindingParent) bindingParents {
		return bindingParents{orm.TableOf(b, &m.ID), orm.ManyOf(b, &m.Children)}
	})
	children := orm.Define("binding_children", func(b orm.Builder[bindingChild], m *bindingChild) bindingChildren {
		return bindingChildren{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.TextOf(b, &m.Slug)}
	})
	parent := modelbinding.Define(func(_ context.Context, id int64) (value.Optional[bindingParent], error) {
		return value.Set(bindingParent{ID: id}), nil
	})
	for _, id := range []int64{1, 2} {
		{
			capture := &goldenCapture{reply: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value) {
				rows := [][]driver.Value(nil)
				if len(args) == 3 && args[0].Value == int64(1) && args[1].Value == "visible" {
					rows = [][]driver.Value{{int64(9), int64(1), "visible"}}
				}
				return []string{"id", "parent_id", "slug"}, rows
			}}
			db := readDB(t, capture)
			var resolver modelbinding.Resolver[int64, modelbinding.Models[bindingParent, bindingChild]]
			rel := orm.HasManyOf(parents.Children, orm.KeyExpr(parents.Key()), orm.NumberExpr(children.ParentID))
			resolver = modelbinding.ThroughRelation(db, parent, rel, orm.TextExpr(children.Slug), func(int64) string { return "visible" })
			result, err := modelbinding.Resolve(resolver, t.Context(), id)

			if !oracleMatch(t, "nested_binding/sql", capture.output.String()) || !oracleMatch(t, "nested_binding/results", oracleSnapshot(result)) || !oracleMatch(t, "nested_binding/absence", oracleSnapshot(err != nil)) || (err != nil) != (id == 2) {
				t.Fatal("nested binding result or authority changed")
			}

			if capture.statements != 1 {
				t.Fatal("nested binding refetched or fell back", capture.statements)
			}
		}

	}
}
