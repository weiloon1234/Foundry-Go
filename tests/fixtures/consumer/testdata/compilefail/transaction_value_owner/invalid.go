package invalid

import (
	"context"
	f "foundry.test/consumer/transactionqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type a struct{}
type b struct{}

var source = query.AsTransaction[a](query.TransactionOf(f.QueryRecords()), "records_alias")
var fields = f.RecordFieldsAt(source.Scope())
var selected = query.SelectTransactionValue(source, fields.ID.Value())
var _ context.Context
var _ *database.Tx

var other = query.AsTransaction[b](query.TransactionOf(f.QueryRecords()), "other")
var _ = selected.Where(query.TransactionInQuery(f.RecordFieldsAt(other.Scope()).ID, selected))
