package expr_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/value"
)

type textLiftState string

const textLiftActive textLiftState = "active"

// EnumDescriptor describes the enum used to check text capability rejection.
func (textLiftState) EnumDescriptor() enum.Descriptor[textLiftState] {
	return enum.Describe("github.com/weiloon1234/Foundry-Go/database/orm/expr_test", "TextLiftState", enum.Case[textLiftState]{Name: "textLiftActive", Value: textLiftActive})
}

type textLiftRecord struct {
	ID       int64
	State    textLiftState
	Nullable value.Nullable[textLiftState]
}
type textLiftFields struct {
	orm.Table[textLiftRecord, int64]
	State    orm.Enum[textLiftRecord, textLiftState]
	Nullable orm.NullableEnum[textLiftRecord, textLiftState]
}

var textLiftTable = orm.Define("text_lift_records", func(b orm.Builder[textLiftRecord], m *textLiftRecord) textLiftFields {
	return textLiftFields{orm.TableOf(b, &m.ID), orm.EnumOf(b, &m.State), orm.NullableEnumOf(b, &m.Nullable)}
})

func TestTextRowValueRendering(t *testing.T) {
	for _, p := range []expr.Predicate[textLiftRecord]{
		expr.TextLike(expr.TextRowValue(expr.Constant[textLiftRecord](codec.String[string](), "MiXeD")), "M%"),
		expr.NullableTextLike(expr.TextNullableRowValue(expr.Constant[textLiftRecord](codec.Nullable(codec.String[string]()), value.Of("MiXeD"))), "M%"),
	} {
		stmt, err := rows.CompileRead(rows.Read(rows.FromModel(expr.Where(textLiftTable.Query(), p))))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stmt.SQL(), "LIKE") || strings.Contains(strings.ToLower(stmt.SQL()), "lower(") {
			t.Fatal(stmt.SQL())
		}
		args := stmt.Arguments()
		if len(args) != 2 || args[0] != "MiXeD" || args[1] != "M%" {
			t.Fatal(args)
		}
	}
}

func TestTextRowValueRejectsNonTextColumns(t *testing.T) {
	if _, err := rows.CompileRead(rows.Read(rows.FromModel(textLiftTable.Query()))); err != nil {
		t.Fatal("invalid test declaration", err)
	}

	for _, p := range []expr.Predicate[textLiftRecord]{
		expr.TextLike(expr.TextRowValue(expr.Value(orm.EnumExpr(textLiftTable.State))), textLiftActive),
		expr.NullableTextLike(expr.TextNullableRowValue(expr.Value(orm.NullableEnumExpr(textLiftTable.Nullable))), textLiftActive),
	} {
		if _, err := rows.CompileRead(rows.Read(rows.FromModel(expr.Where(textLiftTable.Query(), p)))); err == nil {
			t.Fatal("non-text column acquired text comparisons")
		}
	}
}
