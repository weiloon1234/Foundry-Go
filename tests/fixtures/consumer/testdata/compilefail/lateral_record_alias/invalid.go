package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type parent struct{}
type child struct{}
type named struct{}
type other struct{}

var a = rows.As[parent](models.Users.Query(), "a")
var b = rows.As[child](models.Orders.Query(), "b")
var c = rows.Correlate(a, b)
var scope = rows.InnerScope(c, rows.Scope(b))
var selected = rows.SelectCorrelatedRecord(c, scope)
var lateral = rows.AsLateral[named](selected, "lateral")
var _ = rows.AsRecord[named, models.Order](selected, "ordinary")
