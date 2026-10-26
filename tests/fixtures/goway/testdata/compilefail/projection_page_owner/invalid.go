package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "users_alias")
var selected = rows.SelectTransactionValue(source, expr.Row(expr.At(rows.TransactionScope(source), models.Users.ID)).Value())
var c = rows.CursorFor(rows.ModelRecord(models.Users.Query()))
var normal = rows.Read(rows.SelectValue(models.Users.Query(), models.Users.Email))
var input = rows.As[a](models.Users.Query(), "u")
var other = rows.As[b](models.Orders.Query(), "o")

type summary struct{ Name string }
type summaryFields struct {
	rows.Row[summary]
	Name rows.Field[summary, string]
}

var schema = rows.Define(func(b rows.Builder[summary], r *summary) summaryFields {
	return summaryFields{b.Row(), rows.TextOf(b, &r.Name)}
})

func invalid() {
	q := rows.Select(rows.From(models.Users.Query()), schema, rows.Map(schema.Name, expr.Value(models.Users.Email)))
	p, _ := rows.Read(q).Paginate(nil, nil, orm.PageRequest{})
	var _ orm.PageResult[models.User] = p
}
