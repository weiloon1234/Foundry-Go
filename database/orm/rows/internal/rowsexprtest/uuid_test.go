package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type uuidRow struct {
	ID     model.ID[uuidRow]
	Parent value.Nullable[model.ID[uuidRow]]
}
type uuidFields struct {
	orm.Table[uuidRow, model.ID[uuidRow]]
	Parent orm.NullableColumn[uuidRow, model.ID[uuidRow]]
}

func uuidSchema() uuidFields {
	return orm.Define("uuid_rows", func(b orm.Builder[uuidRow], r *uuidRow) uuidFields {
		return uuidFields{orm.TableOf(b, &r.ID), orm.NullableIDOf(b, &r.Parent)}
	})
}
func TestUUIDExpressionAliasAndNullable(t *testing.T) {
	d, foreign := uuidSchema(), uuidSchema()
	a := rows.As[left](d.Query(), "u")
	id := expr.At(rows.Scope(a), orm.KeyExpr(d.Key()))
	parent := expr.At(rows.Scope(a), orm.NullableColumnExpr(d.Parent))
	for _, p := range []expr.Predicate[rows.Alias[left, uuidRow]]{expr.UUIDGreater(id, model.ID[uuidRow]{}), expr.UUIDLessNullable(parent, model.ID[uuidRow]{}), expr.UUIDLessOrEqualNullable(parent, model.ID[uuidRow]{}), expr.UUIDGreaterNullable(parent, model.ID[uuidRow]{}), expr.UUIDGreaterOrEqualNullable(parent, model.ID[uuidRow]{})} {
		expressionSQL(t, expressionQuery(a, expr.AsSelected(expr.Row(id)), codec.ID[uuidRow]()).Where(p))
	}
	bad := expr.At(rows.Scope(a), orm.KeyExpr(foreign.Key()))
	q := expressionQuery(a, expr.AsSelected(expr.Row(id)), codec.ID[uuidRow]()).Where(expr.UUIDGreater(bad, model.ID[uuidRow]{}))
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); err == nil || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}
