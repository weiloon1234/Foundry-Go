package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestResultCursorCompositionPreservesSensitivity(t *testing.T) {
	type secret struct {
		ID   int64
		Name string
	}
	type secretFields struct {
		orm.Table[secret, int64]
		Name orm.Column[secret, string]
	}
	table := orm.Define("secrets", func(b orm.Builder[secret], s *secret) secretFields {
		return secretFields{orm.TableOf(b, &s.ID), orm.ColumnOf(b, &s.Name, orm.Codec(codec.String[string]().WithSensitiveValues()))}
	})
	m, schema := declareMembers(), declareRows()
	public := rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(m.Name))))))
	sensitive := rows.Values(rows.SelectScalar(rows.From(table.Query()), expr.AsSelected(expr.Row(expr.Value(orm.ColumnExpr(table.Name))))))
	report := rows.RecordOf(rows.Select(rows.From(table.Query()), schema, rows.Map(schema.ID, expr.Value(orm.KeyExpr(table.Key()))), rows.Map(schema.Name, expr.Value(orm.ColumnExpr(table.Name)))))
	ordinary := rows.RecordOf(rows.Select(rows.From(m.Query()), schema, rows.Map(schema.ID, expr.Value(orm.KeyExpr(m.ID))), rows.Map(schema.Name, expr.Value(orm.TextExpr(m.Name)))))
	for name, source := range map[string]rows.RecordSource[record]{
		"projection": report, "record set right": rows.UnionAll(ordinary, report), "record set left": rows.UnionAll(report, ordinary), "CTE": rows.CTE("secret_report", report),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			db := openTest(t, state)
			c := rows.CursorFor(source)
			if p, err := c.UniqueBy(expr.Group(rows.At(c.Scope(), schema.Name))).Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" || p.Next.IsSet() || p.Items != nil {
				t.Fatal("sensitive key reached execution", err, state.sql)
			}
			// Sensitivity belongs to each output, not to the whole result.
			state.columns = []string{"id", "name"}
			state.rows = [][]driver.Value{{int64(1), "private"}, {int64(2), "private"}}
			p, err := c.UniqueBy(expr.Group(rows.At(c.Scope(), schema.ID))).Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1})
			if err != nil || !p.Next.IsSet() {
				t.Fatal("public identity rejected", err)
			}
			a := rows.AsRecord[left](source, "aliased_report")
			selected := rows.Values(rows.SelectScalar(a, expr.AsSelected(expr.Row(rows.At(rows.Scope(a), schema.Name)))))
			state.sql = ""
			v := rows.ValueCursorFor(selected)
			if _, err := v.UniqueBy(expr.Group(v.Value())).Paginate(t.Context(), db, orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal("alias/scalar lost sensitivity", err, state.sql)
			}
		})
	}
	for name, source := range map[string]rows.ValueSource[string]{
		"right": rows.UnionAllValues(public, sensitive), "left": rows.UnionAllValues(sensitive, public),
		"nested":    rows.UnionAllValues(public, rows.UnionAllValues(public, sensitive)),
		"intersect": rows.IntersectValues(public, sensitive), "except": rows.ExceptValues(public, sensitive),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			db := openTest(t, state)
			c := rows.ValueCursorFor(source)
			if _, err := c.UniqueBy(expr.Group(c.Value())).Paginate(t.Context(), db, orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
	combined := rows.UnionAllValues(public, sensitive)
	scalar := rows.ValueCursorFor(rows.Values(rows.SelectScalar(combined, expr.AsSelected(expr.Row(combined.Value())))))
	state := &driverState{}
	if _, err := scalar.UniqueBy(expr.Group(scalar.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("set/scalar lost sensitivity", err, state.sql)
	}
	// Reusing the ordinary declaration/source must not inherit another query's flags.
	clean := rows.CursorFor(ordinary)
	cleanState := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(1), "a"}, {int64(2), "b"}}}
	if p, err := clean.UniqueBy(expr.Group(rows.At(clean.Scope(), schema.Name))).Paginate(t.Context(), openTest(t, cleanState), orm.CursorRequest[record]{Size: 1}); err != nil || !p.Next.IsSet() {
		t.Fatal("declaration mutated", err)
	}
}

func TestResultCursorComputedProjectionSensitivity(t *testing.T) {
	type secret struct {
		ID     int64
		Number int64
	}
	type secretFields struct {
		orm.Table[secret, int64]
		Number orm.Column[secret, int64]
	}
	table := orm.Define("secret_numbers", func(b orm.Builder[secret], s *secret) secretFields {
		return secretFields{orm.TableOf(b, &s.ID), orm.ColumnOf(b, &s.Number, orm.Codec(codec.Signed[int64]().WithSensitiveValues()))}
	})
	source := rows.From(table.Query())
	input := expr.Value(orm.ColumnExpr(table.Number))
	for name, selected := range map[string]expr.Selected[secret, int64]{"computed": expr.AsSelected(expr.Add(expr.Value(orm.KeyExpr(table.Key())), input)), "parameter": expr.AsSelected(expr.Add(expr.Value(orm.KeyExpr(table.Key())), expr.Param(orm.ColumnExpr(table.Number), int64(7))))} {
		t.Run(name, func(t *testing.T) {
			c := rows.ValueCursorFor(rows.Values(rows.SelectScalar(source, selected)))
			state := &driverState{}
			if _, err := c.UniqueBy(expr.Group(c.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[int64]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
}
