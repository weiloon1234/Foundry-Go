package goway_test

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
		var transcripts []string
		var results []modelbinding.Models[bindingParent, bindingChild]
		var absent []bool
		for _, old := range []bool{true, false} {
			capture := &goldenCapture{reply: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value) {
				rows := [][]driver.Value(nil)
				if len(args) == 3 && args[0].Value == int64(1) && args[1].Value == "visible" {
					rows = [][]driver.Value{{int64(9), int64(1), "visible"}}
				}
				return []string{"id", "parent_id", "slug"}, rows
			}}
			db := readDB(t, capture)
			var resolver modelbinding.Resolver[int64, modelbinding.Models[bindingParent, bindingChild]]
			if old {
				key := codec.Signed[int64]()
				text := codec.String[string]()
				pq := query.ForModel(query.Define("binding_parents", "id", []query.Column{{Name: "id"}}, func(row database.Row) (bindingParent, error) {
					var m bindingParent
					err := row.Scan(key.Scan(&m.ID))
					return m, err
				}, query.NewModelField("id", key, func(m bindingParent) int64 { return m.ID })))
				cq := query.ForModel(query.Define("binding_children", "id", []query.Column{{Name: "id"}, {Name: "parent_id"}, {Name: "slug"}}, func(row database.Row) (bindingChild, error) {
					var m bindingChild
					err := row.Scan(key.Scan(&m.ID), key.Scan(&m.ParentID), text.Scan(&m.Slug))
					return m, err
				}, query.NewModelField("id", key, func(m bindingChild) int64 { return m.ID }), query.NewModelField("parent_id", key, func(m bindingChild) int64 { return m.ParentID }), query.NewModelField("slug", text, func(m bindingChild) string { return m.Slug })))
				rel := query.HasMany(query.NewScalarField[bindingParent]("binding_parents", "id", key), query.NewScalarField[bindingChild]("binding_children", "parent_id", key)).Bind("Children", pq, cq, func(m bindingParent) relation.Many[bindingChild] { return m.Children }, func(m bindingParent, v relation.Many[bindingChild]) bindingParent { m.Children = v; return m })
				resolver = modelbinding.Through(db, parent, rel, query.NewScalarField[bindingChild]("binding_children", "slug", text), func(int64) string { return "visible" })
			} else {
				rel := orm.HasManyOf(parents.Children, parents.Key(), children.ParentID)
				resolver = modelbinding.ThroughRelation(db, parent, rel, children.Slug, func(int64) string { return "visible" })
			}
			result, err := resolver.Resolve(t.Context(), id)
			results = append(results, result)
			absent = append(absent, err != nil)
			transcripts = append(transcripts, capture.output.String())
			if capture.statements != 1 {
				t.Fatal("nested binding refetched or fell back", capture.statements)
			}
		}
		if !oracleEqual(t, "nested_binding/sql", transcripts[0], transcripts[1]) || !oracleEqual(t, "nested_binding/results", oracleSnapshot(results[0]), oracleSnapshot(results[1])) || !oracleEqual(t, "nested_binding/absence", oracleSnapshot(absent[0]), oracleSnapshot(absent[1])) || !reflect.DeepEqual(results[0], results[1]) || absent[0] != absent[1] || absent[1] != (id == 2) {
			t.Fatal("nested binding parity", transcripts, results, absent)
		}
	}
}
