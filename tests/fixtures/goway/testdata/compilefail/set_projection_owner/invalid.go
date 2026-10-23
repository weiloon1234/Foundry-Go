package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}
type other struct{}

var record = rows.ModelRecord(models.Users.Query())
var cte = rows.CTE("users_copy", record)
var source = rows.AsRecord[tag](cte, "u")
var ids = rows.SelectScalar(source, expr.Select(expr.At(rows.Scope(source), models.Users.ID)))
var nullable = rows.SelectScalar(source, expr.Select(expr.At(rows.Scope(source), models.Users.Nickname)))
var combined = rows.Union(record, record)
var tx = rows.TransactionCTE("claimed", rows.LockedRecord(models.Users.Query().ForUpdate()))

type summary struct{ Email string }
type summaryFields struct {
	rows.Row[summary]
	Email rows.Field[summary, string]
}

var fields = rows.Define(func(b rows.Builder[summary], r *summary) summaryFields {
	return summaryFields{b.Row(), rows.TextOf(b, &r.Email)}
})
var projected = rows.RecordOf(rows.Select(source, fields, rows.Map(fields.Email, expr.At(rows.Scope(source), models.Users.Email))))
var invalid = rows.Union(projected, record)
