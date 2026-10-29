package orm

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
	"strings"
	"testing"
)

type scopeTransferRow struct {
	ID, Rank int64
	Children relation.Many[scopeTransferRow]
	Links    relation.Through[scopeTransferRow, scopeTransferRow]
}
type scopeTransferTable struct {
	Table[scopeTransferRow, int64]
	Rank     Number[scopeTransferRow, int64]
	Children Many[scopeTransferRow, scopeTransferRow]
	Links    Through[scopeTransferRow, scopeTransferRow, scopeTransferRow]
}

func scopeTransferFields(options ...Option) scopeTransferTable {
	return Define("scope_records", func(b Builder[scopeTransferRow], m *scopeTransferRow) scopeTransferTable {
		return scopeTransferTable{TableOf(b, &m.ID), NumberOf(b, &m.Rank), ManyOf(b, &m.Children), ThroughOf(b, &m.Links)}
	}, options...)
}
func transferPositive(d scopeTransferTable) GlobalScope[scopeTransferRow] {
	return Scope(d.Table, "positive", func(context.Context) (Predicate[scopeTransferRow], error) { return NumberGt(d.Rank, 0), nil })
}
func transferTenant(d scopeTransferTable) GlobalScope[scopeTransferRow] {
	return Scope(d.Table, "tenant", func(ctx context.Context) (Predicate[scopeTransferRow], error) {
		n, ok := ctx.Value(scopeTenant{}).(int64)
		if !ok {
			return Predicate[scopeTransferRow]{}, tenantMissing
		}
		return NumberLt(d.Rank, n), nil
	})
}
func TestGlobalScopesJoinEffectivePredicates(t *testing.T) {
	d := scopeTransferFields(WithGlobalScopes(func(d scopeTransferTable) []GlobalScope[scopeTransferRow] {
		return []GlobalScope[scopeTransferRow]{transferPositive(d)}
	}))
	s, e := Compile(WithScopeContext(d.Query(), t.Context()))
	if e != nil || !strings.Contains(s.SQL(), `"rank" > $1`) {
		t.Fatal(s.SQL(), e)
	}
	for _, q := range []Query[scopeTransferRow]{WithoutGlobalScope(d.Query(), transferPositive(d)), WithoutGlobalScopes(d.Query())} {
		s, e := Compile(q)
		if e != nil || len(s.Arguments()) != 0 || strings.Contains(s.SQL(), `"rank" >`) {
			t.Fatal("scope removal", e)
		}
	}
	s2, e := compileRead(WithScopeContext(d.Query(), t.Context()).state, querycore.ReadCount)
	if e != nil || !strings.Contains(s2.SQLText, `"rank" > $1`) {
		t.Fatal("count lost scope", e)
	}
	for _, mode := range []string{"duplicate", "bad name", "zero", "foreign"} {
		bad := scopeTransferFields(WithGlobalScopes(func(d scopeTransferTable) []GlobalScope[scopeTransferRow] {
			if mode == "duplicate" {
				return []GlobalScope[scopeTransferRow]{transferPositive(d), transferPositive(d)}
			}
			return []GlobalScope[scopeTransferRow]{Scope(d.Table, mode, func(context.Context) (Predicate[scopeTransferRow], error) {
				if mode == "zero" {
					return Predicate[scopeTransferRow]{}, nil
				}
				if mode == "foreign" {
					return NumberGt(scopeTransferFields().Rank, 0), nil
				}
				return NumberGt(d.Rank, 0), nil
			})}
		}))
		if _, e := Compile(WithScopeContext(bad.Query(), t.Context())); !errors.Is(e, fault.Invalid) {
			t.Fatal(mode, e)
		}
	}
	if _, e := Compile(WithoutGlobalScope(d.Query(), transferTenant(d))); !errors.Is(e, fault.Invalid) {
		t.Fatal("undeclared scope removal", e)
	}
}
func TestGlobalScopesReachRelationsAndAliases(t *testing.T) {
	parent := scopeTransferFields()
	target := scopeTransferFields(WithGlobalScopes(func(d scopeTransferTable) []GlobalScope[scopeTransferRow] {
		return []GlobalScope[scopeTransferRow]{transferPositive(d), transferTenant(d)}
	}))
	r := HasManyOf(parent.Children, KeyExpr(parent.Key()), NumberExpr(target.Rank))
	ctx := tenantContext(t.Context(), 7)
	s, e := Compile(WithScopeContext(parent.Query().Where(Has(r)), ctx))
	if e != nil || strings.Count(s.SQL(), `."rank" > `) != 1 || strings.Count(s.SQL(), `."rank" < `) != 1 || !reflect.DeepEqual(s.Arguments(), []any{int64(0), int64(7)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	s, e = Compile(WithScopeContext(parent.Query().Where(Has(WithoutRelationGlobalScopes(r))), ctx))
	if e != nil || strings.Contains(s.SQL(), `."rank" >`) || strings.Contains(s.SQL(), `."rank" <`) {
		t.Fatal("target removal", s.SQL(), e)
	}
	links := ManyToManyOf(parent.Links, KeyExpr(parent.Key()), NumberExpr(target.Rank), KeyExpr(target.Key()), KeyExpr(target.Key()))
	s, e = Compile(WithScopeContext(parent.Query().Where(Has(links)), ctx))
	if e != nil || strings.Count(s.SQL(), `."rank" < `) != 2 || strings.Count(s.SQL(), `."rank" > `) != 2 {
		t.Fatal("target/pivot scope qualification", s.SQL(), e)
	}
	if e := Check(parent, links); e != nil {
		t.Fatal("declaration required request context", e)
	}
}
