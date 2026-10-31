package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	id := expr.Value(orm.KeyExpr(d.ID))
	base := rows.FromModel(d.Query())
	q := rows.Offset(rows.Limit(rows.OrderBy(rows.Distinct(base.Where(expr.Ne(id, 7))), expr.Desc(id)), 2), 1)
	s, err := rows.CompileRead(rows.Read(q))
	want := `SELECT DISTINCT "members"."id", "members"."name", "members"."nickname" FROM "members" WHERE ("members"."id" <> $1) ORDER BY 1 DESC LIMIT $2 OFFSET $3`
	if err != nil || s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(2), int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	_ = rows.DistinctOn(q, expr.Group(id))
	again, _ := rows.CompileRead(rows.Read(q))
	if again.SQL() != s.SQL() {
		t.Fatal("derived DISTINCT mutated source")
	}
	original, err := rows.CompileRead(rows.Read(base))
	if err != nil || strings.Contains(original.SQL(), "DISTINCT") {
		t.Fatal("base mutated", err)
	}
	combined := rows.DistinctSet(rows.UnionAll(rows.RecordOf(q), rows.ModelRecord(d.Query())))
	if s, err := rows.CompileRead(rows.ReadSet(combined)); err != nil || strings.Count(s.SQL(), "SELECT DISTINCT ") != 2 {
		t.Fatal("set lost DISTINCT", s.SQL(), err)
	}
	scalar := rows.Values(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Row(id))))
	if s, err := rows.CompileRead(rows.ReadValues(rows.DistinctValues(rows.UnionAllValues(scalar, scalar)))); err != nil || !strings.HasPrefix(s.SQL(), "SELECT DISTINCT ") {
		t.Fatal("value set lost DISTINCT", s.SQL(), err)
	}
}
func TestDistinctOrderingReusesSelectedScalarBindings(t *testing.T) {
	d := declareMembers()
	id := expr.Value(orm.KeyExpr(d.ID))
	base := rows.From(d.Query())
	inner := rows.LimitScalar(rows.WhereScalar(rows.SelectScalar(base, expr.AsSelected(expr.Row(id))), expr.Ne(id, 9)), 1)
	scalar := expr.AsSelected(rows.ScalarRowQuery(base, inner))
	q := rows.LimitScalar(rows.OrderScalar(rows.DistinctScalar(rows.WhereScalar(rows.SelectScalar(base, scalar), expr.Eq(id, 7))), expr.DescValue(scalar)), 2)
	s, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil || strings.Count(s.SQL(), `SELECT "members"."id" AS "value"`) != 1 || !strings.Contains(s.SQL(), "ORDER BY 1 DESC") || !reflect.DeepEqual(s.Arguments(), []any{int64(9), int64(1), int64(7), int64(2)}) {
		t.Fatal("rebound selected scalar", s.SQL(), s.Arguments(), err)
	}
	wrong := expr.AsSelected(rows.ScalarRowQuery(base, rows.LimitScalar(rows.WhereScalar(rows.SelectScalar(base, expr.AsSelected(expr.Row(id))), expr.Ne(id, 8)), 1)))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(base, scalar)), expr.AscValue(wrong)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("different scalar bindings matched", err)
	}
	a := rows.AsRecord[left](rows.CTE("filtered", rows.ModelRecord(d.Query().Where(orm.KeyNe(d.ID, 3)))), "a")
	aid := expr.At(rows.Scope(a), orm.KeyExpr(d.ID))
	if _, err := rows.CompileRead(rows.ReadRecord(rows.DistinctRecord(rows.SelectRecord(a, rows.Scope(a))).OrderBy(expr.Asc(aid)))); err != nil {
		t.Fatal("CTE arguments changed ordinal", err)
	}
	node := rows.SourceOf(q).Node
	other := expr.FoundrySelected(wrong, sqlowner.Expressions()).Node
	node.Selections = append(node.Selections, querycore.SelectItem{Expression: other, Alias: "other"})
	node.Orders = []querycore.OrderNode{{Expression: other}}
	if s, err := querycore.CompileRead(node, t.Context(), false, nil, true); err != nil || !strings.Contains(s.SQLText, "ORDER BY 2 ASC") {
		t.Fatal("absolute placeholders changed ordinal", s.SQLText, err)
	}
}
func TestDistinctOnKeysOrderingAndReplacement(t *testing.T) {
	d := declareMembers()
	id, rank := expr.Value(orm.KeyExpr(d.ID)), expr.Value(orm.NullableTextExpr(d.Nickname))
	base := rows.FromModel(d.Query())
	q := rows.OrderBy(rows.DistinctOn(base, expr.Group(rank)), expr.Asc(rank), expr.Desc(id))
	s, err := rows.CompileRead(rows.Read(q))
	if err != nil || !strings.HasPrefix(s.SQL(), `SELECT DISTINCT ON ("members"."nickname") "members"."id", "members"."name", "members"."nickname"`) || !strings.Contains(s.SQL(), `ORDER BY "members"."nickname" ASC, "members"."id" DESC`) {
		t.Fatal(s.SQL(), err)
	}
	if s, err := rows.CompileRead(rows.Read(rows.Distinct(q))); err != nil || strings.Contains(s.SQL(), "DISTINCT ON") {
		t.Fatal("Distinct did not replace ON", err)
	}
	keys := []expr.GroupKey[member]{expr.Group(id), expr.Group(rank)}
	permuted := rows.OrderBy(rows.DistinctOn(base, keys...), expr.Asc(rank), expr.Desc(id))
	keys[0] = expr.GroupKey[member]{}
	if _, err := rows.CompileRead(rows.Read(permuted)); err != nil {
		t.Fatal("borrowed keys or rejected permutation", err)
	}
	if _, err := rows.CompileRead(rows.Read(rows.OrderBy(rows.DistinctOn(base, expr.Group(id), expr.Group(rank)), expr.Asc(id)))); err != nil {
		t.Fatal("short prefix", err)
	}
	if _, err := rows.CompileRead(rows.Read(rows.DistinctOn(base, expr.Group(rank)))); err != nil {
		t.Fatal("unordered DISTINCT ON", err)
	}
}
func TestDistinctRejectsInvalidSelectionsBeforeExecution(t *testing.T) {
	d, other := declareMembers(), declareMembers()
	id, rank := expr.Value(orm.KeyExpr(d.ID)), expr.Value(orm.NullableTextExpr(d.Nickname))
	base := rows.FromModel(d.Query())
	invalidKind := rows.Distinct(base)
	rows.QuerySource(&invalidKind).Node.Distinct.Kind = 99
	for name, q := range map[string]rows.Query[member, member]{
		"empty keys": rows.DistinctOn(base), "zero key": rows.DistinctOn(base, expr.GroupKey[member]{}), "duplicate": rows.DistinctOn(base, expr.Group(id), expr.Group(id)),
		"foreign": rows.DistinctOn(base, expr.Group(expr.Value(orm.KeyExpr(other.ID)))), "missing metadata": rows.Distinct(rows.FromModel(orm.Query[member]{})),
		"too many": rows.DistinctOn(base, make([]expr.GroupKey[member], querycore.MaxExpressionNodes+1)...),
		"prefix":   rows.OrderBy(rows.DistinctOn(base, expr.Group(rank)), expr.Asc(id), expr.Asc(rank)), "unknown kind": invalidKind,
	} {
		if s, err := rows.CompileRead(rows.Read(q)); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, err)
		}
	}
	scalar := rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Row(id)))
	for _, q := range []rows.Scalar[member, int64]{rows.OrderScalar(rows.DistinctScalar(scalar), expr.Asc(rank)), rows.DistinctOnScalar(rows.GroupByScalar(scalar, expr.Group(id)), expr.Group(rank))} {
		if _, err := rows.CompileRead(rows.ReadScalar(q)); !errors.Is(err, fault.Invalid) {
			t.Fatal("unselected/ungrouped DISTINCT key", err)
		}
	}
}
func TestDistinctCorrelatedKeysFollowOuterQualification(t *testing.T) {
	d := declareMembers()
	base := rows.From(d.Query())
	a := rows.As[left](d.Query(), "inner_record")
	link := rows.Correlate(base, a)
	outer := expr.At(rows.OuterScope(link, rows.ModelScope(d.Query())), orm.KeyExpr(d.ID))
	inner := expr.At(rows.InnerScope(link, rows.Scope(a)), orm.KeyExpr(d.ID))
	q := rows.DistinctOnCorrelatedScalar(rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.Row(inner))).Where(expr.Equal(inner, outer)), expr.Group(outer)).OrderBy(expr.Asc(outer), expr.Asc(inner))
	sub := expr.FoundrySelected(expr.AsSelected(rows.CorrelatedScalarRowQuery(q)), sqlowner.Expressions()).Node.(querycore.ScalarSubquery).Query
	changed := querycore.RequalifyCorrelation(sub, "qualified")
	if changed.Node.Distinct.Keys[0].(querycore.FieldRef).Table != "qualified" || sub.Node.Distinct.Keys[0].(querycore.FieldRef).Table != "members" {
		t.Fatal("correlated key qualification lost source ownership")
	}
	if _, err := orm.Compile(expr.Where(d.Query(), q.Exists())); err != nil {
		t.Fatal("correlated DISTINCT did not compile", err)
	}
}
