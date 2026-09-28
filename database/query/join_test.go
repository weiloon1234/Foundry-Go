package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type firstAlias struct{}
type secondAlias struct{}
type thirdAlias struct{}
type joinedPair struct{ Left, Right value.Nullable[int64] }

func scopedID[S any](scope ModelScope[S, cursorRecord]) ScalarField[S, int64] {
	return NewScalarField[S, int64](scope.Table(), "id", codec.Signed[int64]())
}
func nullableScopedID[S any](scope NullableModelScope[S, cursorRecord]) NullableField[S, int64] {
	return NewNullableField[S, int64](scope.Table(), "id", codec.Signed[int64]())
}
func pairProjection[S any](source ProjectionSource[S], left, right Expression[S, value.Nullable[int64]]) ProjectionQuery[S, joinedPair] {
	l, r := NewProjectionField[joinedPair, value.Nullable[int64]]("left"), NewProjectionField[joinedPair, value.Nullable[int64]]("right")
	d := DefineProjection([]ProjectionColumn[joinedPair]{l.Column(), r.Column()}, func(database.Row) (joinedPair, error) { return joinedPair{}, nil })
	return Project(source, d, Map(l, left), Map(r, right))
}

func TestJoinSourceWindowsAndParameterOrder(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	a := As[firstAlias](cursorQuery().Where(id.Gt(1)).Limit(2), "a")
	b := As[secondAlias](cursorQuery().Where(id.Lt(9)).OrderBy(id.Desc()).Offset(1), "b")
	x, y := scopedID(a.Scope()), scopedID(b.Scope())
	on := On(x, y).WhereLeft(x.Ne(5)).WhereRight(y.Ne(6))
	j := LeftJoin(a, b, on)
	left, right := scopedID(LeftScope(j, a.Scope())), nullableScopedID(NullableRightScope(j, b.Scope()))
	q := pairProjection(j, Nullable(left.Value()), right.Value()).Where(left.Ne(7)).OrderBy(left.Asc()).Limit(4)
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`FROM (SELECT "records"."id", "records"."rank" FROM "records" WHERE ("records"."id" > $1) LIMIT $2) AS "a"`,
		`LEFT JOIN (SELECT "records"."id", "records"."rank" FROM "records" WHERE ("records"."id" < $3) ORDER BY "records"."id" DESC OFFSET $4) AS "b"`,
		`ON ((("a"."id" = "b"."id") AND ("a"."id" <> $5)) AND ("b"."id" <> $6)) WHERE ("a"."id" <> $7) ORDER BY "a"."id" ASC LIMIT $8`,
	} {
		if !strings.Contains(s.SQL(), want) {
			t.Fatal("source semantics or parameter order changed", s.SQL())
		}
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(9), int64(1), int64(5), int64(6), int64(7), int64(4)}) {
		t.Fatal("wrong joined arguments", s.Arguments())
	}
	// Deriving another chain must not append sources to the earlier report.
	c := As[thirdAlias](cursorQuery(), "c")
	chain := InnerJoin(j, c, On(right, scopedID(c.Scope())))
	chainRight := nullableScopedID(LeftNullableScope(chain, NullableRightScope(j, b.Scope())))
	chainLeft := scopedID(LeftScope(chain, LeftScope(j, a.Scope())))
	if _, err := pairProjection(chain, Nullable(chainLeft.Value()), chainRight.Value()).Compile(); err != nil {
		t.Fatal(err)
	}
	original, err := q.Compile()
	if err != nil || original.SQL() != s.SQL() {
		t.Fatal("join chain mutated earlier source", err)
	}
}

func TestJoinInvalidAliasesScopesAndDescriptors(t *testing.T) {
	a := As[firstAlias](cursorQuery(), "a")
	b := As[secondAlias](cursorQuery(), "b")
	x, y := scopedID(a.Scope()), scopedID(b.Scope())
	valid := InnerJoin(a, b, On(x, y))
	var nilSource *AliasedSource[firstAlias, cursorRecord]
	for name, source := range map[string]InnerJoined[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]]{
		"missing ON":          InnerJoin(a, b, JoinOn[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]]{}),
		"nil source":          InnerJoin(nilSource, b, On(x, y)),
		"invalid alias":       InnerJoin(a, As[secondAlias](cursorQuery(), "bad.alias"), On(x, y)),
		"repeated alias name": InnerJoin(a, As[secondAlias](cursorQuery(), "a"), On(x, y)),
		"metadata missing":    InnerJoin(a, As[secondAlias](For[cursorRecord]("records"), "b"), On(x, y)),
		"eager input":         InnerJoin(a, As[secondAlias](cursorQuery().With(testRelation(cursorQuery(), cursorQuery())), "b"), On(x, y)),
	} {
		l, r := scopedID(LeftScope(source, a.Scope())), scopedID(RightScope(source, b.Scope()))
		if _, err := pairProjection(source, Nullable(l.Value()), Nullable(r.Value())).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	duplicate := As[firstAlias](cursorQuery(), "another")
	repeated := InnerJoin(a, duplicate, On(x, scopedID(duplicate.Scope())))
	if repeated.input.err == nil {
		t.Fatal("same typed alias identity was accepted twice")
	}
	// Same Go alias type with a different runtime name cannot contribute fields
	// to a source it does not belong to, including through a join scope helper.
	wrong := scopedID(LeftScope(valid, duplicate.Scope()))
	right := scopedID(RightScope(valid, b.Scope()))
	if _, err := pairProjection(valid, Nullable(wrong.Value()), Nullable(right.Value())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unregistered alias contributed fields", err)
	}
	plain := pairProjection(a, Nullable(x.Value()), Nullable(x.Value()))
	if _, err := plain.Where(scopedID(duplicate.Scope()).Eq(1)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("same-tag wrong alias predicate compiled", err)
	}
}

func TestDerivedSelectRejectsCyclesAndOuterReferences(t *testing.T) {
	node := selectNode{source: tableSource{alias: "nested", columns: []Column{{Name: "id"}}}}
	node.source.query = &node
	var c compiler
	if _, err := c.selectSQL(node); !errors.Is(err, fault.Invalid) {
		t.Fatal("cyclic SELECT was not bounded", err)
	}
	inner := cursorQuery().modelSelect()
	inner.predicates = []expression{NewScalarField[cursorRecord, int64]("outer", "id", codec.Signed[int64]()).Eq(1).expression}
	outer := selectNode{source: tableSource{alias: "outer", columns: []Column{{Name: "id"}}, query: &inner}}
	c = compiler{}
	if _, err := c.selectSQL(outer); !errors.Is(err, fault.Invalid) {
		t.Fatal("derived source accidentally correlated to outer scope", err)
	}
}
