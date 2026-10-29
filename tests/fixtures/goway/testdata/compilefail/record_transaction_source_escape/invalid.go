package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
var projected = rows.Select(input, fields, rows.Map(fields.Name, expr.Value(orm.TextExpr(models.Users.Email))))
var alias = rows.AsRecord[tag](rows.RecordOf(projected), "summary")
var parent = rows.As[tag](models.Users.Query(), "parent")
var child = rows.As[otherTag](models.Users.Query(), "child")
var joined = rows.LeftJoin(parent, child, rows.On(expr.At(rows.Scope(parent), orm.KeyExpr(models.Users.ID)), expr.At(rows.Scope(child), orm.KeyExpr(models.Users.ID))))
var txSource = rows.AsTransaction[tag](rows.LockedRecord(orm.Lock(models.Users.Query())), "claimed")
var txScope = rows.TransactionScope(txSource)
var selected = rows.SelectTransactionRecord(txSource, txScope)

var invalid = rows.AsRecord[otherTag](selected, "escaped")
