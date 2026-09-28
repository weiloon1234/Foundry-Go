package invalid

import (
	"context"
	f "foundry.test/consumer/transactionqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type a struct{}
type b struct{}
type other struct{}
type result struct{}

var parent = query.AsTransaction[a](query.TransactionOf(f.QueryRecords()), "parent")
var child = query.AsTransaction[b](query.TransactionOf(f.QueryRecords()), "child")
var correlation = query.TransactionCorrelate(parent, child)
var fields = f.RecordFieldsAt(query.InnerScope(correlation, child.Scope()))
var selected = query.SelectTransactionCorrelatedValue(correlation, fields.ID.Value()).ForUpdate()
var record = query.SelectTransactionCorrelatedRecord(correlation, query.InnerScope(correlation, child.Scope())).ForShare()
var lateral = query.AsTransactionLateral[result](record, "lateral_record")
var _ context.Context
var _ *database.Tx

var _ = selected.All
