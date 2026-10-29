package goway_test

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
)

func TestReportRelationAdapterRejectsForeignDeclaration(t *testing.T) {
	foreign := orm.Define("orders", func(b orm.Builder[models.Order], o *models.Order) models.OrderTable {
		table := orm.TableOf(b, &o.ID)
		return models.OrderTable{Table: table, ID: table.Key(), BuyerID: orm.IDOf(b, &o.BuyerID), TotalCents: orm.NumberOf(b, &o.TotalCents), Buyer: orm.OneOf(b, &o.Buyer)}
	})
	p := expr.Has(models.UserRelations.Orders, expr.ModelPredicate(orm.NumberGt(foreign.TotalCents, 1)))
	if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(models.Users.Query()).Where(p))); err == nil {
		t.Fatal("foreign child declaration accepted")
	}
	if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(models.Users.Query()).Where(expr.Has(models.UserRelations.Orders)))); err != nil {
		t.Fatal(err)
	}
}

func TestReportEnumPatternCompilerDefense(t *testing.T) {
	type record struct {
		ID    model.ID[record]
		State models.Status
		Maybe value.Nullable[models.Status]
	}
	type fields struct {
		orm.Table[record, model.ID[record]]
		State orm.Enum[record, models.Status]
		Maybe orm.NullableEnum[record, models.Status]
	}
	f := orm.Define("report_enum_records", func(b orm.Builder[record], r *record) fields {
		return fields{orm.TableOf(b, &r.ID), orm.EnumOf(b, &r.State), orm.NullableEnumOf(b, &r.Maybe)}
	})
	state, maybe := expr.Value(orm.EnumExpr(f.State)), expr.Value(orm.NullableEnumExpr(f.Maybe))
	grouped := rowset.GroupBy(rowset.FromModel(f.Query()), expr.Group(expr.Value(orm.KeyExpr(f.Key()))), expr.Group(state), expr.Group(maybe))
	for _, p := range []expr.HavingPredicate[record]{
		expr.TextValue(expr.Row(state).Value()).Contains(models.StatusActive),
		expr.TextValue(expr.Row(state).Value()).Like(models.StatusActive),
		expr.TextNullableValue(expr.Row(maybe).Value()).Contains(models.StatusActive),
		expr.TextNullableValue(expr.Row(maybe).Value()).Like(models.StatusActive),
	} {
		if _, err := rowset.CompileRead(rowset.Read(rowset.Having(grouped, p))); err == nil || !strings.Contains(err.Error(), "text capability") {
			t.Fatalf("enum selected pattern: %v", err)
		}
	}
	for _, p := range []expr.Predicate[record]{expr.Like(state, state), expr.LikeNullable(maybe, maybe)} {
		if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(f.Query()).Where(p))); err == nil || !strings.Contains(err.Error(), "text capability") {
			t.Fatalf("enum binary pattern: %v", err)
		}
	}
	// Equality and membership are still valid for required and nullable enums.
	if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(f.Query()).Where(expr.CompareRow(state).In(models.StatusActive), expr.CompareNullableRow(maybe).Eq(models.StatusActive)))); err != nil {
		t.Fatal(err)
	}
}
