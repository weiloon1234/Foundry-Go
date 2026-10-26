package invalid

import (
	"context"
	f "foundry.test/consumer/transactionqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}

var source = rows.AsTransaction[a](rows.TransactionCTE("claimed", rows.LockedRecord(f.Records.Query().ForUpdate())), "claim")
var selected = rows.SelectTransactionRecord(source, rows.TransactionScope(source))
var _ context.Context
var _ *database.Tx

var _ = rows.SelectTransaction(source, f.Summaries, rows.MapSelected(f.Summaries.ID, expr.Row(expr.At(rows.TransactionScope(source), f.Records.Name)).Value()))
