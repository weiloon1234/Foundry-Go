package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
)

func TestDistinctKeepsSelectedRowsAndParameters(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	base := FromModel(d.Query())
	q := Offset(Limit(OrderBy(Distinct(base.Where(id.Ne(7))), id.Desc()), 2), 1)
	s, err := CompileRead(Read(q))
	want := `SELECT DISTINCT "members"."id", "members"."name", "members"."nickname" FROM "members" WHERE ("members"."id" <> $1) ORDER BY 1 DESC LIMIT $2 OFFSET $3`
	if err != nil || s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(2), int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	_ = DistinctOn(q, expr.Group(id))
	again, _ := CompileRead(Read(q))
	if again.SQL() != s.SQL() {
		t.Fatal("derived DISTINCT mutated source")
	}
	original, err := CompileRead(Read(base))
	if err != nil || strings.Contains(original.SQL(), "DISTINCT") {
		t.Fatal("base mutated", err)
	}
	combined := DistinctSet(UnionAll(RecordOf(q), ModelRecord(d.Query())))
	if s, err := CompileRead(ReadSet(combined)); err != nil || strings.Count(s.SQL(), "SELECT DISTINCT ") != 2 {
		t.Fatal("set lost DISTINCT", s.SQL(), err)
	}
	scalar := Values(SelectScalar(From(d.Query()), expr.Row(id).Value()))
	if s, err := CompileRead(ReadValues(DistinctValues(UnionAllValues(scalar, scalar)))); err != nil || !strings.HasPrefix(s.SQL(), "SELECT DISTINCT ") {
		t.Fatal("value set lost DISTINCT", s.SQL(), err)
	}
}
func TestDistinctOrderingReusesSelectedScalarBindings(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	base := From(d.Query())
	inner := LimitScalar(WhereScalar(SelectScalar(base, expr.Row(id).Value()), id.Ne(9)), 1)
	scalar := ScalarRowQuery(base, inner).Value()
	q := LimitScalar(OrderScalar(DistinctScalar(WhereScalar(SelectScalar(base, scalar), id.Eq(7))), scalar.Desc()), 2)
	s, err := CompileRead(ReadScalar(q))
	if err != nil || strings.Count(s.SQL(), `SELECT "members"."id" AS "value"`) != 1 || !strings.Contains(s.SQL(), "ORDER BY 1 DESC") || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(1), int64(7), int64(2)}) {
		t.Fatal("rebound selected scalar", s.SQL(), s.Arguments(), err)
	}
	wrong := ScalarRowQuery(base, LimitScalar(WhereScalar(SelectScalar(base, expr.Row(id).Value()), id.Ne(8)), 1)).Value()
	if _, err := CompileRead(ReadScalar(OrderScalar(DistinctScalar(SelectScalar(base, scalar)), wrong.Asc()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("different scalar bindings matched", err)
	}
	a := AsRecord[left](CTE("filtered", ModelRecord(d.Query().Where(d.ID.Ne(3)))), "a")
	aid := expr.At(Scope(a), d.ID)
	if _, err := CompileRead(ReadRecord(DistinctRecord(SelectRecord(a, Scope(a))).OrderBy(aid.Asc()))); err != nil {
		t.Fatal("CTE arguments changed ordinal", err)
	}
	node := q.source.Node
	other := expr.FoundrySelected(wrong, sqlowner.Expressions()).Node
	node.Selections = append(node.Selections, querycore.SelectItem{Expression: other, Alias: "other"})
	node.Orders = []querycore.OrderNode{{Expression: other}}
	if s, err := querycore.CompileRead(node, t.Context(), false, nil, true); err != nil || !strings.Contains(s.SQLText, "ORDER BY 2 ASC") {
		t.Fatal("absolute placeholders changed ordinal", s.SQLText, err)
	}
}
func TestDistinctOnKeysOrderingAndReplacement(t *testing.T) {
	d := declareMembers()
	id, rank := expr.Value(d.ID), expr.Value(d.Nickname)
	base := FromModel(d.Query())
	q := OrderBy(DistinctOn(base, expr.Group(rank)), rank.Asc(), id.Desc())
	s, err := CompileRead(Read(q))
	if err != nil || !strings.HasPrefix(s.SQL(), `SELECT DISTINCT ON ("members"."nickname") "members"."id", "members"."name", "members"."nickname"`) || !strings.Contains(s.SQL(), `ORDER BY "members"."nickname" ASC, "members"."id" DESC`) {
		t.Fatal(s.SQL(), err)
	}
	if s, err := CompileRead(Read(Distinct(q))); err != nil || strings.Contains(s.SQL(), "DISTINCT ON") {
		t.Fatal("Distinct did not replace ON", err)
	}
	keys := []expr.GroupKey[member]{expr.Group(id), expr.Group(rank)}
	permuted := OrderBy(DistinctOn(base, keys...), rank.Asc(), id.Desc())
	keys[0] = expr.GroupKey[member]{}
	if _, err := CompileRead(Read(permuted)); err != nil {
		t.Fatal("borrowed keys or rejected permutation", err)
	}
	if _, err := CompileRead(Read(OrderBy(DistinctOn(base, expr.Group(id), expr.Group(rank)), id.Asc()))); err != nil {
		t.Fatal("short prefix", err)
	}
	if _, err := CompileRead(Read(DistinctOn(base, expr.Group(rank)))); err != nil {
		t.Fatal("unordered DISTINCT ON", err)
	}
}
func TestDistinctRejectsInvalidSelectionsBeforeExecution(t *testing.T) {
	d, other := declareMembers(), declareMembers()
	id, rank := expr.Value(d.ID), expr.Value(d.Nickname)
	base := FromModel(d.Query())
	invalidKind := Distinct(base)
	invalidKind.source.Node.Distinct.Kind = 99
	for name, q := range map[string]Query[member, member]{
		"empty keys": DistinctOn(base), "zero key": DistinctOn(base, expr.GroupKey[member]{}), "duplicate": DistinctOn(base, expr.Group(id), expr.Group(id)),
		"foreign": DistinctOn(base, expr.Group(expr.Value(other.ID))), "missing metadata": Distinct(FromModel(orm.Query[member]{})),
		"too many": DistinctOn(base, make([]expr.GroupKey[member], querycore.MaxExpressionNodes+1)...),
		"prefix":   OrderBy(DistinctOn(base, expr.Group(rank)), id.Asc(), rank.Asc()), "unknown kind": invalidKind,
	} {
		if s, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, err)
		}
	}
	scalar := SelectScalar(From(d.Query()), expr.Row(id).Value())
	for _, q := range []Scalar[member, int64]{OrderScalar(DistinctScalar(scalar), rank.Asc()), DistinctOnScalar(GroupByScalar(scalar, expr.Group(id)), expr.Group(rank))} {
		if _, err := CompileRead(ReadScalar(q)); !errors.Is(err, fault.Invalid) {
			t.Fatal("unselected/ungrouped DISTINCT key", err)
		}
	}
}
func TestDistinctCorrelatedKeysFollowOuterQualification(t *testing.T) {
	d := declareMembers()
	base := From(d.Query())
	a := As[left](d.Query(), "inner_record")
	link := Correlate(base, a)
	outer := expr.At(OuterScope(link, ModelScope(d.Query())), d.ID)
	inner := expr.At(InnerScope(link, Scope(a)), d.ID)
	q := DistinctOnCorrelatedScalar(SelectCorrelatedScalar(link, expr.Row(inner).Value()).Where(expr.Equal(inner, outer)), expr.Group(outer)).OrderBy(outer.Asc(), inner.Asc())
	sub := expr.FoundrySelected(CorrelatedScalarRowQuery(q).Value(), sqlowner.Expressions()).Node.(querycore.ScalarSubquery).Query
	changed := querycore.RequalifyCorrelation(sub, "qualified")
	if changed.Node.Distinct.Keys[0].(querycore.FieldRef).Table != "qualified" || sub.Node.Distinct.Keys[0].(querycore.FieldRef).Table != "members" {
		t.Fatal("correlated key qualification lost source ownership")
	}
	if _, err := orm.Compile(expr.Where(d.Query(), q.Exists())); err != nil {
		t.Fatal("correlated DISTINCT did not compile", err)
	}
}
