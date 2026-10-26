package rows

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
	public := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.Name)).Value()))
	sensitive := Values(SelectScalar(From(table.Query()), expr.Row(expr.Value(table.Name)).Value()))
	report := RecordOf(Select(From(table.Query()), schema, Map(schema.ID, expr.Value(table.Key())), Map(schema.Name, expr.Value(table.Name))))
	ordinary := RecordOf(Select(From(m.Query()), schema, Map(schema.ID, expr.Value(m.ID)), Map(schema.Name, expr.Value(m.Name))))
	for name, source := range map[string]RecordSource[record]{
		"projection": report, "record set right": UnionAll(ordinary, report), "record set left": UnionAll(report, ordinary), "CTE": CTE("secret_report", report),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			db := openTest(t, state)
			c := CursorFor(source)
			if p, err := c.UniqueBy(expr.Group(At(c.Scope(), schema.Name))).Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" || p.Next.IsSet() || p.Items != nil {
				t.Fatal("sensitive key reached execution", err, state.sql)
			}
			// Sensitivity belongs to each output, not to the whole result.
			state.columns = []string{"id", "name"}
			state.rows = [][]driver.Value{{int64(1), "private"}, {int64(2), "private"}}
			p, err := c.UniqueBy(expr.Group(At(c.Scope(), schema.ID))).Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1})
			if err != nil || !p.Next.IsSet() {
				t.Fatal("public identity rejected", err)
			}
			a := AsRecord[left](source, "aliased_report")
			selected := Values(SelectScalar(a, expr.Row(At(Scope(a), schema.Name)).Value()))
			state.sql = ""
			v := ValueCursorFor(selected)
			if _, err := v.UniqueBy(expr.Group(v.Value())).Paginate(t.Context(), db, orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal("alias/scalar lost sensitivity", err, state.sql)
			}
		})
	}
	for name, source := range map[string]ValueSource[string]{
		"right": UnionAllValues(public, sensitive), "left": UnionAllValues(sensitive, public),
		"nested":    UnionAllValues(public, UnionAllValues(public, sensitive)),
		"intersect": IntersectValues(public, sensitive), "except": ExceptValues(public, sensitive),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			db := openTest(t, state)
			c := ValueCursorFor(source)
			if _, err := c.UniqueBy(expr.Group(c.Value())).Paginate(t.Context(), db, orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
	combined := UnionAllValues(public, sensitive)
	scalar := ValueCursorFor(Values(SelectScalar(combined, expr.Row(combined.Value()).Value())))
	state := &driverState{}
	if _, err := scalar.UniqueBy(expr.Group(scalar.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("set/scalar lost sensitivity", err, state.sql)
	}
	// Reusing the ordinary declaration/source must not inherit another query's flags.
	clean := CursorFor(ordinary)
	cleanState := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(1), "a"}, {int64(2), "b"}}}
	if p, err := clean.UniqueBy(expr.Group(At(clean.Scope(), schema.Name))).Paginate(t.Context(), openTest(t, cleanState), orm.CursorRequest[record]{Size: 1}); err != nil || !p.Next.IsSet() {
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
	source := From(table.Query())
	input := expr.Value(table.Number)
	for name, selected := range map[string]expr.Selected[secret, int64]{"computed": expr.Add(expr.Value(table.Key()), input).Value(), "parameter": expr.Add(expr.Value(table.Key()), expr.Param(table.Number, int64(7))).Value()} {
		t.Run(name, func(t *testing.T) {
			c := ValueCursorFor(Values(SelectScalar(source, selected)))
			state := &driverState{}
			if _, err := c.UniqueBy(expr.Group(c.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[int64]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
}
