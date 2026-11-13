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
	"github.com/weiloon1234/Foundry-Go/value"
)

type collatedLabel string

func TestCollationBuildValidation(t *testing.T) {
	input := expr.Constant[textLiftRecord](codec.String[collatedLabel](), "adam")
	for _, collation := range []expr.Collation{"", "bad\x00name", "bad\xffname", expr.Collation(strings.Repeat("a", 64)), expr.ICUCollation(""), expr.ICUCollation("en_US"), expr.ICUCollation("é"), expr.ICUCollation("en.US"), expr.ICUCollation("en\nUS"), expr.ICUCollation(strings.Repeat("a", 58))} {
		row := expr.Collate(input, collation)
		query := expr.OrderBy(textLiftTable.Query(), expr.Asc(row)).Limit(0)
		if _, err := rows.CompileRead(rows.Read(rows.FromModel(query))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid collation survived query build", err)
		}
	}
	for _, tag := range []string{"en-US", "zh-Hant-TW", "und", "es-419"} {
		if got := expr.ICUCollation(tag); got != expr.Collation(tag+"-x-icu") {
			t.Fatal("ICU name", got)
		}
	}
	for _, collation := range []expr.Collation{expr.CollationC, expr.CollationUnicode, expr.ICUCollation(strings.Repeat("a", 57)), "unknown_but_valid"} {
		query := expr.OrderBy(textLiftTable.Query(), expr.Asc(expr.Collate(input, collation)))
		if _, err := rows.CompileRead(rows.Read(rows.FromModel(query))); err != nil {
			t.Fatal("valid name requires no database existence check", err)
		}
	}
}

func TestCollationPreservesTypesCodecsAndInputErrors(t *testing.T) {
	rejected := errors.New("label rejected")
	c := codec.String[collatedLabel]().Validated(func(v collatedLabel) error {
		if v == "invalid" {
			return rejected
		}
		return nil
	})
	input := expr.Constant[textLiftRecord](c, "adam")
	var row expr.TextRowExpression[textLiftRecord, collatedLabel] = expr.Collate(input, expr.CollationC)
	var selected expr.Selected[textLiftRecord, collatedLabel] = expr.CollateValue(expr.AsSelected(input), expr.CollationC)
	var nullable expr.NullableTextRowExpression[textLiftRecord, collatedLabel] = expr.CollateNullable(expr.NullableRow(input), expr.CollationC)
	var nullableSelected expr.Selected[textLiftRecord, value.Nullable[collatedLabel]] = expr.CollateNullableValue(expr.AsSelected(expr.NullableRow(input)), expr.CollationC)
	for _, query := range []orm.Query[textLiftRecord]{
		expr.Where(textLiftTable.Query(), expr.TextLike(row, "invalid")),
		expr.Where(textLiftTable.Query(), expr.NullableTextLike(nullable, "invalid")),
		expr.OrderBy(textLiftTable.Query(), expr.Asc(expr.ParamOf(selected, "invalid"))),
		expr.OrderBy(textLiftTable.Query(), expr.Asc(expr.ParamOf(nullableSelected, value.Of(collatedLabel("invalid"))))),
		expr.OrderBy(textLiftTable.Query(), expr.Asc(expr.Collate(expr.Constant[textLiftRecord](c, "invalid"), expr.CollationC))),
	} {
		if _, err := rows.CompileRead(rows.Read(rows.FromModel(query))); !errors.Is(err, rejected) {
			t.Fatal("collation lost input codec or error", err)
		}
	}
	for _, p := range []expr.Predicate[textLiftRecord]{
		expr.TextLike(expr.Collate(expr.Value(orm.EnumExpr(textLiftTable.State)), expr.CollationC), textLiftActive),
		expr.NullableTextLike(expr.CollateNullable(expr.Value(orm.NullableEnumExpr(textLiftTable.Nullable)), expr.CollationC), textLiftActive),
	} {
		if _, err := rows.CompileRead(rows.Read(rows.FromModel(expr.Where(textLiftTable.Query(), p)))); !errors.Is(err, fault.Invalid) {
			t.Fatal("enum acquired text capability", err)
		}
	}
}
