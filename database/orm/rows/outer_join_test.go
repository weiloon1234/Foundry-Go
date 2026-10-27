package rows

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type outerRecord struct {
	ID       value.Nullable[int64]
	Nickname value.Nullable[string]
}
type outerFields struct {
	Row[outerRecord]
	ID       Field[outerRecord, value.Nullable[int64]]
	Nickname Field[outerRecord, value.Nullable[string]]
}

func outerSchema() outerFields {
	return Define(func(b Builder[outerRecord], r *outerRecord) outerFields {
		return outerFields{b.Row(), NullableNumberOf(b, &r.ID), NullableTextOf(b, &r.Nickname)}
	})
}
func TestOuterJoinNullabilityAndKinds(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	on := On(expr.At(Scope(a), m.ID), expr.At(Scope(b), m.ID))
	l, r, f := LeftJoin(a, b, on), RightJoin(a, b, on), FullJoin(a, b, on)
	s := outerSchema()
	project := func(source Source[Left[Alias[left, member], Alias[right, member]]]) Query[Left[Alias[left, member], Alias[right, member]], outerRecord] {
		scope := NullableRightScope(l, Scope(b))
		return Select(source, s, Map(s.ID, expr.AtNullable(scope, m.ID)), Map(s.Nickname, expr.AtNullableValue(scope, m.Nickname)))
	}
	state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}, {int64(3), "nick"}}}
	got, err := Read(project(l)).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].ID.IsNull() || !got[0].Nickname.IsNull() || got[1].ID != value.Of(int64(3)) {
		t.Fatal(got, err)
	}
	if !strings.Contains(state.sql, `LEFT JOIN "members" AS "b"`) {
		t.Fatal(state.sql)
	}
	queries := []struct {
		kind string
		run  func(*driverState) error
	}{
		{"RIGHT", func(state *driverState) error {
			scope := NullableLeftScope(r, Scope(a))
			_, err := Read(Select(r, s, Map(s.ID, expr.AtNullable(scope, m.ID)), Map(s.Nickname, expr.AtNullableValue(scope, m.Nickname)))).All(t.Context(), openTest(t, state))
			return err
		}},
		{"FULL", func(state *driverState) error {
			x, y := NullableLeftScope(f, Scope(a)), NullableRightScope(f, Scope(b))
			_, err := Read(Select(f, s, Map(s.ID, expr.AtNullable(x, m.ID)), Map(s.Nickname, expr.AtNullableValue(y, m.Nickname)))).All(t.Context(), openTest(t, state))
			return err
		}},
	}
	for _, q := range queries {
		t.Run(q.kind, func(t *testing.T) {
			state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}}}
			if err := q.run(state); err != nil || !strings.Contains(state.sql, q.kind+` JOIN "members"`) {
				t.Fatal(state.sql, err)
			}
		})
	}
}
func TestNullableScopeChainAndSourceBoundaries(t *testing.T) {
	m := declareMembers()
	o := declareOrders()
	a, b := As[left](m.Query().Where(m.Name.Eq("Ada")).Limit(2), "a"), As[right](o.Query().Where(o.MemberID.Gt(0)), "b")
	joined := LeftJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), o.MemberID)))
	type third struct{}
	c := As[third](m.Query(), "c")
	chain := CrossJoin(joined, c)
	scope := LeftNullableScope(chain, NullableRightScope(joined, Scope(b)))
	type one struct{ ID value.Nullable[int64] }
	type fields struct {
		Row[one]
		ID Field[one, value.Nullable[int64]]
	}
	schema := Define(func(b Builder[one], r *one) fields { return fields{b.Row(), NullableNumberOf(b, &r.ID)} })
	e := expr.AtNullable(scope, o.ID)
	core := expr.FoundryExpression(e, sqlowner.Expressions())
	if err := ormcore.CheckReferences(chain.source, core.Refs); err != nil {
		t.Fatal(err)
	}
	state := &driverState{columns: []string{"id"}, rows: [][]driver.Value{{nil}}}
	q := Select(chain, schema, Map(schema.ID, e)).Where(e.IsNull())
	if _, err := Read(q).All(t.Context(), openTest(t, state)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.sql, `CROSS JOIN`) || !strings.Contains(state.sql, `LIMIT $2`) || !strings.Contains(state.sql, `"orders" WHERE`) {
		t.Fatal(state.sql)
	}
	state = &driverState{}
	other := declareOrders()
	bad := Select(chain, schema, Map(schema.ID, expr.AtNullable(scope, other.ID)))
	if _, err := Read(bad).All(t.Context(), openTest(t, state)); err == nil || state.sql != "" {
		t.Fatal("foreign declaration accepted", err)
	}
}
func TestJoinRuntimeOwnershipFailures(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query(), "a")
	b := As[right](m.Query(), "b")
	on := On(expr.At(Scope(a), m.ID), expr.At(Scope(b), m.ID))
	for _, j := range []LeftJoined[Alias[left, member], Alias[right, member]]{
		LeftJoin(a, As[right](m.Query(), "a"), on),
		LeftJoin(a, b, JoinOn[Alias[left, member], Alias[right, member]]{}),
		LeftJoin(a, b, On(expr.At(Scope(As[left](m.Query(), "other")), m.ID), expr.At(Scope(b), m.ID))),
	} {
		if j.source.Err == nil {
			t.Fatal("invalid join accepted")
		}
	}
	same := CrossJoin(a, As[left](m.Query(), "other"))
	if same.source.Err == nil {
		t.Fatal("repeated typed alias accepted")
	}
	j := LeftJoin(a, b, on)
	wrong := NullableRightScope(j, Scope(As[right](m.Query(), "other")))
	if expr.FoundryNullableScopeData(wrong, sqlowner.Expressions()).Err == nil {
		t.Fatal("same typed foreign alias accepted")
	}
	var missing *LeftJoined[Alias[left, member], Alias[right, member]]
	if expr.FoundryScopeData(LeftScope(missing, Scope(a)), sqlowner.Expressions()).Err == nil {
		t.Fatal("nil join accepted")
	}
}
