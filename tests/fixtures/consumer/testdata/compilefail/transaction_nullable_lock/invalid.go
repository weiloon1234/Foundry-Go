package invalid

import (
	"context"
	f "foundry.test/consumer/transactionqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type a struct{}
type b struct{}

var source = query.AsTransaction[a](query.TransactionCTE("claimed", f.QueryRecords().ForUpdate()), "claim")
var selected = query.SelectTransactionRecord(source, source.Scope())
var _ context.Context
var _ *database.Tx

var other = query.AsTransaction[b](query.TransactionOf(f.QueryRecords()), "other")
var joined = query.TransactionLeftJoin(source, other, query.On(f.RecordFieldsAt(source.Scope()).ID, f.RecordFieldsAt(other.Scope()).ID))
var _ = query.UpdateLock(query.NullableRightScope(joined, other.Scope()))
