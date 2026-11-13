package expr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConditionalCollationRequiresEveryResultInput(t *testing.T) {
	plain := expr.Constant[textLiftRecord](codec.String[textLiftState](), textLiftActive)
	nullablePlain := expr.NullableRow(plain)
	marked := expr.Row(expr.Value(orm.EnumExpr(textLiftTable.State)))
	nullableMarked := expr.Row(expr.Value(orm.NullableEnumExpr(textLiftTable.Nullable)))
	p, n, m, mn := expr.AsSelected(plain), expr.AsSelected(nullablePlain), expr.AsSelected(marked), expr.AsSelected(nullableMarked)
	condition := expr.Eq(expr.Value(orm.KeyExpr(textLiftTable.Key())), int64(1))
	groupCondition := expr.Grouped(condition)
	window := expr.WindowFor(rows.ModelScope(textLiftTable.Query()))
	for name, input := range map[string]expr.Selected[textLiftRecord, value.Nullable[textLiftState]]{
		"coalesce first":             expr.Nullable(expr.AsSelected(expr.Coalesce(nullableMarked, plain))),
		"coalesce fallback":          expr.Nullable(expr.CoalesceValue(n, m)),
		"nullable coalesce later":    expr.AsSelected(expr.CoalesceNullable(nullablePlain, nullablePlain, nullableMarked)),
		"nullable selected coalesce": expr.CoalesceNullableValue(n, mn),
		"nullif second":              expr.AsSelected(expr.NullIf(plain, marked)),
		"nullif first":               expr.NullIfValue(m, p),
		"nullable nullif second":     expr.AsSelected(expr.NullIfNullable(nullablePlain, nullableMarked)),
		"nullable selected nullif":   expr.NullIfNullableValue(n, mn),
		"case first":                 expr.Nullable(expr.AsSelected(expr.When(condition, marked).Else(plain))),
		"case middle":                expr.Nullable(expr.AsSelected(expr.When(condition, plain).When(condition, marked).Else(plain))),
		"case else":                  expr.Nullable(expr.AsSelected(expr.When(condition, plain).Else(marked))),
		"case else null":             expr.AsSelected(expr.When(condition, plain).When(condition, marked).ElseNull()),
		"selected case middle":       expr.Nullable(expr.WhenValue(groupCondition, p).When(groupCondition, m).Else(p)),
		"selected case else":         expr.Nullable(expr.WhenValue(groupCondition, p).Else(m)),
		"selected case else null":    expr.WhenValue(groupCondition, p).When(groupCondition, m).ElseNull(),
		"nested conditionals":        expr.Nullable(expr.CoalesceValue(expr.AsSelected(expr.When(condition, plain).ElseNull()), expr.CoalesceValue(mn, p))),
		"lag fallback":               expr.Nullable(expr.LagOr(m, 1, textLiftActive, window)),
		"lead fallback":              expr.Nullable(expr.LeadOr(m, 1, textLiftActive, window)),
	} {
		t.Run(name, func(t *testing.T) {
			source := rows.From(textLiftTable.Query())
			if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.CollateNullableValue(input, expr.CollationC)))); !errors.Is(err, fault.Invalid) {
				t.Fatal("composition accepted explicit collation", err)
			}
			order := expr.AscValue(expr.FoundryDefaultCollation(input, expr.CollationUnicode, sqlowner.Expressions()))
			statement, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(source, input), order)))
			if err != nil || strings.Contains(statement.SQL(), "COLLATE") {
				t.Fatal("composition acquired default collation", statement.SQL(), err)
			}
		})
	}
	// A marked predicate does not determine CASE's result type, and deriving a
	// marked branch must not change a previously built all-text CASE.
	base := expr.When(expr.Eq(marked, textLiftActive), plain)
	_ = base.When(condition, marked).Else(plain)
	for _, input := range []expr.Selected[textLiftRecord, textLiftState]{
		expr.AsSelected(base.Else(plain)), expr.CoalesceValue(n, p),
		expr.CoalesceValue(expr.NullIfValue(p, p), p),
	} {
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(textLiftTable.Query()), expr.CollateValue(input, expr.CollationC)))); err != nil {
			t.Fatal("all-text composition lost collation", err)
		}
	}
}

func TestTextCastParametersIgnoreExemplarCollationMark(t *testing.T) {
	rejected := errors.New("invalid label")
	c := codec.String[collatedLabel]().WithoutCollation().Validated(func(v collatedLabel) error {
		if v == "invalid" {
			return rejected
		}
		return nil
	})
	constant := expr.Constant[textLiftRecord](c, "adam")
	for _, input := range []expr.RowExpression[textLiftRecord, collatedLabel]{constant, expr.ParamOf(constant, "bob")} {
		collated := expr.Collate(input, expr.CollationUnicode)
		statement, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(textLiftTable.Query()), expr.AsSelected(collated))))
		if err != nil || !strings.Contains(statement.SQL(), `CAST($1 AS text) COLLATE "und-x-icu"`) {
			t.Fatal("text cast inherited non-collatable mark", statement.SQL(), err)
		}
		invalid := expr.ParamOf(collated, "invalid")
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(textLiftTable.Query()), expr.AsSelected(invalid)))); !errors.Is(err, rejected) {
			t.Fatal("cast lost parameter validation", err)
		}
		column := expr.FoundrySelectedColumn(expr.AsSelected(collated), sqlowner.Expressions())
		var decoded collatedLabel
		if err := column.Scan(&decoded).(interface{ Scan(any) error }).Scan("invalid"); !errors.Is(err, rejected) {
			t.Fatal("cast lost decoding validation", err)
		}
	}
	parameter := expr.Param(orm.EnumExpr(textLiftTable.State), textLiftActive)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(textLiftTable.Query()), expr.AsSelected(expr.Collate(parameter, expr.CollationC))))); err != nil {
		t.Fatal("text-cast enum parameter rejected collation", err)
	}
}
