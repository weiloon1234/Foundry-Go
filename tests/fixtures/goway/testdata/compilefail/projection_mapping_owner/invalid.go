package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}
type otherTag struct{}
type summary struct{ Name string }
type summaryFields struct {
	rows.Row[summary]
	Name rows.Field[summary, string]
}

var fields = rows.Define(func(b rows.Builder[summary], r *summary) summaryFields {
	return summaryFields{b.Row(), rows.TextOf(b, &r.Name)}
})
var input = rows.From(models.Users.Query())
var projected = rows.Select(input, fields, rows.Map(fields.Name, expr.Value(models.Users.Email)))
var alias = rows.AsRecord[tag](rows.RecordOf(projected), "summary")
var parent = rows.As[tag](models.Users.Query(), "parent")
var child = rows.As[otherTag](models.Users.Query(), "child")
var joined = rows.LeftJoin(parent, child, rows.On(expr.At(rows.Scope(parent), models.Users.ID), expr.At(rows.Scope(child), models.Users.ID)))
var txSource = rows.AsTransaction[tag](rows.LockedRecord(models.Users.Query().ForUpdate()), "claimed")
var txScope = rows.TransactionScope(txSource)
var selected = rows.SelectTransactionRecord(txSource, txScope)

type other struct{ Count int64 }

var otherFields = rows.Define(func(b rows.Builder[other], r *other) struct {
	rows.Row[other]
	Count rows.Field[other, int64]
} {
	return struct {
		rows.Row[other]
		Count rows.Field[other, int64]
	}{b.Row(), rows.NumberOf(b, &r.Count)}
})
var invalid = rows.Select(input, fields, rows.MapSelected(otherFields.Count, expr.Count[models.User]().Value()))
