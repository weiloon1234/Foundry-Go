package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	f "foundry.test/consumer/transactionqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}
type other struct{}
type result struct{}

var parent = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(f.Records.Query())), "parent")
var child = rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(f.Records.Query())), "child")
var correlation = rows.TransactionCorrelate(parent, child)
var fields = rows.TransactionInnerScope(correlation, rows.TransactionScope(child))
var selected = rows.SelectTransactionCorrelatedScalar(correlation, expr.AsSelected(expr.Row(expr.At(fields, orm.KeyExpr(f.Records.ID))))).ForUpdate()
var record = rows.SelectTransactionCorrelatedRecord(correlation, rows.TransactionInnerScope(correlation, rows.TransactionScope(child))).ForShare()
var lateral = rows.AsTransactionLateral[result](record, "lateral_record")
var _ context.Context
var _ *database.Tx

var _ = rows.CorrelatedSource[rows.TransactionAlias[a, f.Record], rows.TransactionAlias[b, f.Record]](correlation)
