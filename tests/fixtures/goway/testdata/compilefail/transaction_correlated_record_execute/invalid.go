package invalid

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
var id = expr.At(parentScope, models.Users.ID)
var childID = expr.At(childScope, models.Users.ID)
var selected = rows.SelectTransactionValue(child, expr.Select(childID))
var correlation = rows.TransactionCorrelate(parent, child)
var innerScope = rows.TransactionInnerScope(correlation, childScope)
var innerID = expr.At(innerScope, models.Users.ID)
var correlated = rows.SelectTransactionCorrelatedScalar(correlation, expr.Select(innerID)).ForUpdate()
var record = rows.SelectTransactionCorrelatedRecord(correlation, innerScope).ForShare()
var lateral = rows.AsTransactionLateral[result](record, "latest")
var joined = rows.TransactionLeftJoin(parent, child, rows.On(id, childID))
var normal = rows.As[a](models.Users.Query(), "ordinary")
var normalChild = rows.As[b](models.Users.Query(), "ordinary_child")
var normalJoin = rows.LeftJoin(normal, normalChild, rows.On(expr.At(rows.Scope(normal), models.Users.ID), expr.At(rows.Scope(normalChild), models.Users.ID)))

var invalid = record.All
