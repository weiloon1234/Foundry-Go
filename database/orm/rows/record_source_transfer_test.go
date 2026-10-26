package rows

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestProjectionDerivedSourcesPreserveOutputAndWindows(t *testing.T) {
	type summary struct{ ID, Count int64 }
	type columns struct {
		Row[summary]
		ID, Count Field[summary, int64]
	}
	schema := Define(func(b Builder[summary], r *summary) columns {
		return columns{b.Row(), NumberOf(b, &r.ID), NumberOf(b, &r.Count)}
	})
	m := declareMembers()
	id := expr.Value(m.ID)
	count := expr.Count[member]()
	report := Select(From(m.Query()), schema, Map(schema.ID, id), MapSelected(schema.Count, count.Value())).Where(expr.OrderRow(id).Gt(2))
	report = Limit(OrderBy(Having(GroupBy(report, expr.Group(id)), count.Gt(3)), count.Desc()), 4)
	source := AsRecord[left](RecordOf(report), "totals")
	total := At(Scope(source), schema.Count)
	s, err := CompileRead(ReadScalar(LimitScalar(WhereScalar(SelectScalar(source, total.Value()), expr.OrderRow(total).Gt(5)), 6)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), `FROM (SELECT "members"."id" AS "id", COUNT(*) AS "count"`) ||
		!strings.Contains(s.SQL(), `HAVING (COUNT(*) > $2) ORDER BY COUNT(*) DESC LIMIT $3) AS "totals" WHERE ("totals"."count" > $4) LIMIT $5`) ||
		!reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(4), int64(5), int64(6)}) {
		t.Fatal("derived output/filter/window mismatch", s.SQL(), s.Arguments())
	}
	bad := AsRecord[left](RecordOf(Select(From(m.Query()), schema)), "bad")
	if _, err := CompileRead(ReadScalar(SelectScalar(bad, total.Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("incomplete derived projection accepted", err)
	}
	forged := SelectScalar(source, total.Value())
	forged.source.Node.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "members", Column: "id"}}}
	if _, err := CompileRead(ReadScalar(forged)); !errors.Is(err, fault.Invalid) {
		t.Fatal("inner model field escaped output boundary", err)
	}
	type pair struct{ A, B value.Nullable[int64] }
	type pairColumns struct {
		Row[pair]
		A, B Field[pair, value.Nullable[int64]]
	}
	pairSchema := Define(func(b Builder[pair], r *pair) pairColumns {
		return pairColumns{b.Row(), NullableNumberOf(b, &r.A), NullableNumberOf(b, &r.B)}
	})
	projected := RecordOf(Select(From(m.Query()), pairSchema, MapSelected(pairSchema.A, expr.NullableRow(id).Value()), MapSelected(pairSchema.B, expr.NullableRow(id).Value())))
	cols := projected.core.Record.Columns
	if len(cols) != 2 || !cols[0].Nullable || !cols[1].Nullable || RecordOf(report).core.Record.Columns[0].Nullable {
		t.Fatal("derived output nullability lost")
	}
}

func TestProjectionCompleteTypedMappingAndSourceIsolation(t *testing.T) {
	type summary struct{ ID, Count int64 }
	type columns struct {
		Row[summary]
		ID, Count Field[summary, int64]
	}
	schema := Define(func(b Builder[summary], r *summary) columns {
		return columns{b.Row(), NumberOf(b, &r.ID), NumberOf(b, &r.Count)}
	})
	m := declareMembers()
	id := expr.Value(m.ID)
	count := expr.Count[member]()
	mappings := []Mapping[member, summary]{MapSelected(schema.Count, count.Value()), Map(schema.ID, id)}
	base := Select(From(m.Query().OrderBy(m.Key().Asc())), schema, mappings...)
	q := GroupBy(base, expr.Group(id))
	mappings[0] = Mapping[member, summary]{}
	s, err := CompileRead(Read(q))
	if err != nil || s.SQL() != `SELECT "members"."id" AS "id", COUNT(*) AS "count" FROM "members" GROUP BY "members"."id" ORDER BY "members"."id" ASC` {
		t.Fatal(s.SQL(), err)
	}
	limited, err := CompileRead(Read(Offset(Limit(q, 2), 1)))
	if err != nil || !reflect.DeepEqual(limited.Arguments(), []any{int64(2), int64(1)}) {
		t.Fatal(limited.Arguments(), err)
	}
	again, _ := CompileRead(Read(q))
	if again.SQL() != s.SQL() {
		t.Fatal("projection derivation mutated source")
	}
	foreign := schema.ID
	fc := *foreign.core
	fc.column.Typ = reflect.TypeFor[string]()
	foreign.core = &fc
	for _, bad := range []Query[member, summary]{
		Select(From(m.Query()), schema, Map(schema.ID, id)),
		Select(From(m.Query()), schema, Map(schema.ID, id), Map(schema.ID, id)),
		Select(From(m.Query()), schema, Map(schema.ID, id), MapSelected(schema.Count, expr.Selected[member, int64]{})),
		Select(From(m.Query()), schema, Map(foreign, id), MapSelected(schema.Count, count.Value())),
		base, GroupBy(q, expr.Group(id)), Limit(q, -1),
	} {
		if _, err := CompileRead(Read(bad)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid mapping/grouping accepted", err)
		}
	}
}

func TestProjectionRejectsEagerLoads(t *testing.T) {
	type model struct {
		ID       int64
		Children relation.Many[model]
	}
	type table struct {
		orm.Table[model, int64]
		Children orm.Many[model, model]
	}
	d := orm.Define("projection_parents", func(b orm.Builder[model], m *model) table {
		return table{orm.TableOf(b, &m.ID), orm.ManyOf(b, &m.Children)}
	})
	r := orm.HasManyOf(d.Children, d.Key(), d.Key())
	if err := orm.Check(r); err != nil {
		t.Fatal(err)
	}
	schema := expressionSchema(codec.Signed[int64]())
	q := Select(From(d.Query().With(r)), schema, Map(schema.Value, expr.Value(d.Key())))
	if _, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) {
		t.Fatal("projection silently lost eager load", err)
	}
	if _, err := CompileRead(Read(Distinct(q))); !errors.Is(err, fault.Invalid) {
		t.Fatal("DISTINCT lost eager load", err)
	}
	source := From(d.Query().With(r))
	base := From(d.Query())
	id := expr.Value(d.Key())
	if _, err := CompileRead(ReadScalar(WhereScalar(SelectScalar(base, expr.Row(id).Value()), ExistsQuery(base, source)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("EXISTS lost eager load", err)
	}
	if _, err := CompileRead(ReadRecord(SelectRecord(source, ModelScope(d.Query())))); !errors.Is(err, fault.Invalid) {
		t.Fatal("record lost eager load", err)
	}
	if _, err := CompileRead(ReadSet(Union(ModelRecord(d.Query()), ModelRecord(d.Query().With(r))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("set lost eager load", err)
	}
	a, b := As[left](d.Query(), "a"), As[right](d.Query().With(r), "b")
	j := InnerJoin(a, b, On(expr.At(Scope(a), d.Key()), expr.At(Scope(b), d.Key())))
	if _, err := CompileRead(ReadRecord(SelectRecord(j, LeftScope(j, Scope(a))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("join lost eager load", err)
	}
}
