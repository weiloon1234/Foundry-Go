package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCursorTerminalsValidateWithoutPageBoundary(t *testing.T) {
	m := declareMembers()
	base := rows.CursorFor(rows.ModelRecord(m.Query().Where(orm.KeyNe(m.ID, 0)).OrderBy(orm.KeyDesc(m.ID)).Limit(8).Offset(1)))
	id := expr.At(base.Scope(), orm.KeyExpr(m.ID))
	valid := base.UniqueBy(expr.Group(id))
	s, err := rows.CompileCursor(valid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s.SQL(), "LIMIT") != 1 || !strings.Contains(s.SQL(), `ORDER BY "foundry_cursor"."id" ASC`) {
		t.Fatal("compile added a page or lost canonical order", s.SQL())
	}
	if err := rows.PreflightCursor(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightCursor(nil, valid); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rows.PreflightCursor(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	foreign := declareMembers()
	for _, bad := range []rows.CursorQuery[member]{
		{}, base, base.UniqueBy(expr.Group(id), expr.Group(id)),
		base.UniqueBy(expr.Group(expr.Row(id).Value().Param(1))),
		base.UniqueBy(expr.Group(expr.At(base.Scope(), orm.KeyExpr(foreign.ID)))),
		valid.OrderBy(expr.Negate(id).Asc()),
		valid.OrderBy(id.Asc().NullsLast()),
	} {
		if _, err := rows.CompileCursor(bad); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid cursor compiled", err)
		}
		if err := rows.PreflightCursor(t.Context(), bad); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid cursor preflight succeeded", err)
		}
	}
	again, err := rows.CompileCursor(valid)
	if err != nil || again.SQL() != s.SQL() {
		t.Fatal("validation mutated cursor", err)
	}
	scalar := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))).Value())))
	scalar = scalar.UniqueBy(expr.Group(scalar.Value()))
	if _, err := rows.CompileValueCursor(scalar); err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(t.Context(), scalar); err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(nil, scalar); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(ctx, scalar); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	badScalar := scalar.UniqueBy(expr.Group(scalar.Value().Param(value.Null[string]())))
	if _, err := rows.CompileValueCursor(badScalar); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(t.Context(), badScalar); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestCursorPreflightUsesCurrentScopeContext(t *testing.T) {
	m := orm.Define("members", func(b orm.Builder[member], m *member) members {
		table := orm.TableOf(b, &m.ID)
		return members{table, orm.PrimaryOf(b, table.Key()), orm.TextOf(b, &m.Name), orm.NullableTextOf(b, &m.Nickname)}
	}, orm.WithGlobalScopes(func(d members) []orm.GlobalScope[member] {
		return []orm.GlobalScope[member]{orm.Scope(d.Table, "tenant", func(ctx context.Context) (orm.Predicate[member], error) {
			id, ok := ctx.Value(reportTenantKey{}).(int64)
			if !ok {
				return orm.Predicate[member]{}, errors.New("missing tenant")
			}
			return orm.KeyEq(d.ID, id), nil
		})}
	}))
	bound := context.WithValue(t.Context(), reportTenantKey{}, int64(1))
	cursor := rows.CursorFor(rows.ModelRecord(orm.WithScopeContext(m.Query(), bound)))
	cursor = cursor.UniqueBy(expr.Group(expr.At(cursor.Scope(), orm.KeyExpr(m.ID))))
	statement, err := rows.CompileCursor(cursor)
	if err != nil || statement.Arguments()[0] != int64(1) {
		t.Fatal(statement, err)
	}
	if err := rows.PreflightCursor(context.WithValue(t.Context(), reportTenantKey{}, int64(2)), cursor); err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightCursor(t.Context(), cursor); err == nil {
		t.Fatal("cursor preflight inherited stale context")
	}
}
