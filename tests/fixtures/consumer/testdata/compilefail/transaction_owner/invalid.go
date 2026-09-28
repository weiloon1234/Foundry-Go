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
var _ = selected.Where(f.RecordFieldsAt(other.Scope()).ID.Eq(1))
