package rows

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type latestTag struct{}
type lateralTotal struct{ Count int64 }
type lateralTotalFields struct {
	Row[lateralTotal]
	Count Field[lateralTotal, int64]
}

func lateralTotalSchema() lateralTotalFields {
	return Define(func(b Builder[lateralTotal], r *lateralTotal) lateralTotalFields {
		return lateralTotalFields{b.Row(), NumberOf(b, &r.Count)}
	})
}

func TestLateralModelNullabilityAndWindows(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	c := Correlate(a, b)
	parent, child := expr.At(OuterScope(c, Scope(a)), m.ID), expr.At(InnerScope(c, Scope(b)), m.ID)
	record := SelectCorrelatedRecord(c.Where(expr.Less(child, parent)), InnerScope(c, Scope(b))).OrderBy(child.Desc()).Limit(1)
	latest := AsLateral[latestTag](record, "latest")
	j := LeftJoinLateral(a, latest)
	nullable := NullableRightScope(j, LateralScope(latest))
	schema := outerSchema()
	q := Select(j, schema, Map(schema.ID, expr.AtNullable(nullable, m.ID)), Map(schema.Nickname, expr.AtNullableValue(nullable, m.Nickname)))
	state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}, {int64(3), "name"}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].ID.IsNull() || got[1].ID.IsNull() {
		t.Fatal(got, err)
	}
	for _, part := range []string{`LEFT JOIN LATERAL (SELECT`, `"b"."id" < "a"."id"`, `ORDER BY "b"."id" DESC LIMIT $1) AS "latest" ON TRUE`} {
		if !strings.Contains(state.sql, part) {
			t.Fatal(state.sql)
		}
	}
	if !reflect.DeepEqual(state.arguments, []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}) {
		t.Fatal("lateral binding order", state.arguments)
	}
	_ = record.Offset(3)
	if record.core.Correlation.Source.Node.Offset != 0 {
		t.Fatal("window mutated original")
	}
	for _, source := range []querycore.JoinKind{querycore.InnerJoin, querycore.CrossJoin} {
		node := q.source.Node
		node.Joins = append([]querycore.JoinNode(nil), node.Joins...)
		node.Joins[0].Kind = source
		q2 := q
		q2.source.Node = node
		if _, err := Read(q2).All(t.Context(), openTest(t, &driverState{columns: []string{"id", "nickname"}})); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLateralProjectionFieldsAndOwnership(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	c := Correlate(a, b)
	totals := lateralTotalSchema()
	record := ProjectCorrelated(c, totals, MapSelected(totals.Count, expr.Count[Correlation[Alias[left, member], Alias[right, member]]]().Value()))
	latest := AsLateral[latestTag](record, "latest")
	j := LeftJoinLateral(a, latest)
	field := AtNullable(NullableRightScope(j, LateralScope(latest)), totals.Count)
	schema := correlationCountSchema()
	state := &driverState{columns: []string{"count"}, rows: [][]driver.Value{{nil}, {int64(2)}}}
	got, err := Read(Select(j, schema, MapSelected(schema.Count, expr.Row(field).Value()))).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].Count.IsNull() {
		t.Fatal(got, err)
	}
	other := lateralTotalSchema()
	if !errors.Is(expr.FoundryRowValue(At(LateralScope(latest), other.Count), sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("foreign projection field accepted")
	}
	wrongParent := As[left](declareMembers().Query(), "a")
	invalid := LeftJoinLateral(wrongParent, latest)
	if !errors.Is(invalid.source.Err, fault.Invalid) {
		t.Fatal("foreign preceding declaration accepted")
	}
	empty := AsLateral[latestTag](CorrelatedRecord[Alias[left, member], Alias[right, member], member]{}, "missing")
	if !errors.Is(empty.source.Err, fault.Invalid) {
		t.Fatal("empty record accepted")
	}
	outerRecord := SelectCorrelatedRecord(c, OuterScope(c, Scope(a)))
	if !errors.Is(outerRecord.core.Correlation.Source.Err, fault.Invalid) {
		t.Fatal("outer record selected as inner")
	}
}

func TestLateralSharedCompilerRejectsMissingForwardAndUnsupportedJoins(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	c := Correlate(a, b)
	lat := AsLateral[latestTag](SelectCorrelatedRecord(c, InnerScope(c, Scope(b))).Limit(0), "latest")
	j := LeftJoinLateral(a, lat)
	schema := declareRows()
	valid := Select(j, schema, Map(schema.ID, expr.At(LeftScope(j, Scope(a)), m.ID)), Map(schema.Name, expr.At(LeftScope(j, Scope(a)), m.Name)))
	for _, kind := range []string{"right", "full", "missing", "self", "forward", "not_derived", "cross_on"} {
		t.Run(kind, func(t *testing.T) {
			q := valid
			q.source.Node.Joins = append([]querycore.JoinNode(nil), valid.source.Node.Joins...)
			join := &q.source.Node.Joins[0]
			switch kind {
			case "right":
				join.Kind = querycore.RightJoin
			case "full":
				join.Kind = querycore.FullJoin
			case "missing":
				join.Lateral = &querycore.ScopeRequirement{}
			case "self":
				join.Lateral = &querycore.ScopeRequirement{Sources: map[string][]querycore.Column{"latest": join.Source.Columns}}
			case "forward":
				join.Lateral = &querycore.ScopeRequirement{Sources: map[string][]querycore.Column{"future": join.Source.Columns}}
				q.source.Node.Joins = append(q.source.Node.Joins, querycore.JoinNode{Kind: querycore.CrossJoin, Source: querycore.TableSource{Table: "members", Alias: "future", Columns: join.Source.Columns}})
			case "cross_on":
				join.Kind = querycore.CrossJoin
				join.On = querycore.BinaryComparison{Left: querycore.FieldRef{Table: "a", Column: "id"}, Right: querycore.FieldRef{Table: "a", Column: "id"}, Operator: querycore.Equal}
			case "not_derived":
				join.Source.Query = nil
			}
			state := &driverState{}
			if _, err := Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
				t.Fatal(err, state.sql)
			}
		})
	}
}

func TestLateralInvalidJoinPredicate(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	c := Correlate(a, b)
	latest := AsLateral[latestTag](SelectCorrelatedRecord(c, InnerScope(c, Scope(b))), "latest")
	wrong := LeftJoinLateral(As[left](m.Query(), "different"), latest)
	if _, e := CompileRead(ReadRecord(SelectRecord(wrong, LeftScope(wrong, Scope(a))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("wrong outer scope", e)
	}
	bad := LeftJoinLateral(a, latest, JoinOn[Alias[left, member], Alias[latestTag, member]]{})
	if _, e := CompileRead(ReadRecord(SelectRecord(bad, LeftScope(bad, Scope(a))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("zero ON", e)
	}
}
