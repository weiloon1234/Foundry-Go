package query

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type scopeTenantKey struct{}

var (
	scopeRank      = NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	positiveScope  = NewGlobalScope[cursorRecord]("positive", scopeRank.Gt(0))
	tenantScope    = NewContextScope[cursorRecord]("tenant", tenantPredicate)
	undeclaredRank = NewGlobalScope[cursorRecord]("undeclared", scopeRank.Gt(9))
)

func tenantPredicate(ctx context.Context) (Predicate[cursorRecord], error) {
	tenant, ok := ctx.Value(scopeTenantKey{}).(int64)
	if !ok {
		return Predicate[cursorRecord]{}, fault.New(fault.Invalid, "tenant missing")
	}
	return scopeRank.Lt(tenant), nil
}

func scopedQuery(scopes ...GlobalScope[cursorRecord]) Query[cursorRecord] {
	d := *cursorQuery().definition
	return ForModel(d.WithGlobalScopes(scopes...))
}

func TestGlobalScopesJoinEffectivePredicates(t *testing.T) {
	q := scopedQuery(positiveScope)
	statement, err := q.Compile()
	if err != nil || !strings.Contains(statement.SQL(), `WHERE ("records"."rank" > $1)`) {
		t.Fatal("static scope not applied", statement.SQL(), err)
	}
	for _, unscoped := range []Query[cursorRecord]{q.WithoutGlobalScope(positiveScope), q.WithoutGlobalScopes()} {
		statement, err := unscoped.Compile()
		if err != nil || strings.Contains(statement.SQL(), "WHERE") {
			t.Fatal("scope removal ignored", statement.SQL(), err)
		}
	}
	if statement, err := q.compile(readCount); err != nil || !strings.Contains(statement.SQL(), `"records"."rank" > $1`) {
		t.Fatal("count ignored scope", statement.SQL(), err)
	}
	if _, err := q.WithoutGlobalScope(undeclaredRank).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("removing an undeclared scope was accepted")
	}
	for _, bad := range [][]GlobalScope[cursorRecord]{
		{positiveScope, positiveScope},
		{NewGlobalScope[cursorRecord]("bad name", scopeRank.Gt(0))},
		{NewGlobalScope[cursorRecord]("empty", Predicate[cursorRecord]{})},
		{NewGlobalScope[cursorRecord]("foreign", NewScalarField[cursorRecord, int64]("other", "id", codec.Signed[int64]()).Eq(1))},
	} {
		if _, err := scopedQuery(bad...).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid scope declaration accepted", err)
		}
	}
}

func TestContextScopesResolveFromTheExecutingContext(t *testing.T) {
	q := scopedQuery(tenantScope)
	if _, err := q.Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "tenant") {
		t.Fatal("unresolved context scope compiled", err)
	}
	ctx := context.WithValue(t.Context(), scopeTenantKey{}, int64(5))
	statement, err := q.WithScopeContext(ctx).Compile()
	if err != nil || !strings.Contains(statement.SQL(), `"records"."rank" < $1`) || !reflect.DeepEqual(statement.Arguments(), []any{int64(5)}) {
		t.Fatal("context scope not resolved", statement.SQL(), statement.Arguments(), err)
	}
	// The executing context wins over a stale bound context.
	executing := context.WithValue(t.Context(), scopeTenantKey{}, int64(8))
	statement, err = q.WithScopeContext(ctx).compileIn(executing)
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(8)}) {
		t.Fatal("stale scope context reused", statement.Arguments(), err)
	}
	if _, err := q.WithScopeContext(t.Context()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing tenant did not fail closed")
	}
	panicking := scopedQuery(NewContextScope[cursorRecord]("panics", func(context.Context) (Predicate[cursorRecord], error) { panic("scope bug") }))
	if _, err := panicking.WithScopeContext(ctx).Compile(); err == nil {
		t.Fatal("scope panic escaped as success")
	}
	if statement, err := q.WithoutGlobalScope(tenantScope).Compile(); err != nil || strings.Contains(statement.SQL(), "WHERE") {
		t.Fatal("context scope removal ignored", statement.SQL(), err)
	}
}

func TestGlobalScopesReachRelationsAndAliases(t *testing.T) {
	scoped := scopedQuery(positiveScope, tenantScope)
	ctx := context.WithValue(t.Context(), scopeTenantKey{}, int64(7))
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	r := testRelation(cursorQuery(), scoped)
	statement, err := cursorQuery().WhereHas(r).compileIn(ctx)
	if err != nil || strings.Count(statement.SQL(), `."rank" > `) != 1 || strings.Count(statement.SQL(), `."rank" < `) != 1 {
		t.Fatal("WhereHas ignored target scopes", statement.SQL(), err)
	}
	if statement, err := cursorQuery().WhereHas(r.WithoutGlobalScopes()).compileIn(ctx); err != nil || strings.Contains(statement.SQL(), `."rank" >`) || strings.Contains(statement.SQL(), `."rank" <`) {
		t.Fatal("relation scope removal ignored", statement.SQL(), err)
	}
	through := ManyToMany(id, id, id, id).Bind("Links", cursorQuery(), scoped, scoped, testThrough().get, testThrough().set)
	statement, err = through.compileThrough(ctx, id.In(1).expression, 3)
	if err != nil || !strings.Contains(statement.SQL(), `"foundry_target"."rank" < `) || !strings.Contains(statement.SQL(), `"foundry_pivot"."rank" < `) {
		t.Fatal("through relation did not requalify scopes", statement.SQL(), err)
	}
	if err := cursorQuery().With(through).Validate(); err != nil {
		t.Fatal("context scopes broke declaration validation", err)
	}
}
