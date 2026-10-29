package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}
type other struct{}
type result struct{}

var _ context.Context
var _ *database.Tx
var parent = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "parent")
var child = rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "child")
var different = rows.AsTransaction[other](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "different")
var parentScope = rows.TransactionScope(parent)
var childScope = rows.TransactionScope(child)
var id = expr.At(parentScope, orm.KeyExpr(models.Users.ID))
var childID = expr.At(childScope, orm.KeyExpr(models.Users.ID))
var selected = rows.SelectTransactionValue(child, expr.Row(childID).Value())
var correlation = rows.TransactionCorrelate(parent, child)
var innerScope = rows.TransactionInnerScope(correlation, childScope)
var innerID = expr.At(innerScope, orm.KeyExpr(models.Users.ID))
var correlated = rows.SelectTransactionCorrelatedScalar(correlation, expr.Row(innerID).Value()).ForUpdate()
var record = rows.SelectTransactionCorrelatedRecord(correlation, innerScope).ForShare()
var lateral = rows.AsTransactionLateral[result](record, "latest")
var joined = rows.TransactionLeftJoin(parent, child, rows.On(id, childID))
var normal = rows.As[a](models.Users.Query(), "ordinary")
var normalChild = rows.As[b](models.Users.Query(), "ordinary_child")
var normalJoin = rows.LeftJoin(normal, normalChild, rows.On(expr.At(rows.Scope(normal), orm.KeyExpr(models.Users.ID)), expr.At(rows.Scope(normalChild), orm.KeyExpr(models.Users.ID))))

var invalid = rows.CorrelatedSource[rows.TransactionAlias[a, models.User], rows.TransactionAlias[b, models.User]](correlation)
