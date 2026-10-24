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

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(f.Records.Query())), "records_alias")
var fields = rows.TransactionScope(source)
var selected = rows.SelectTransactionValue(source, expr.Select(expr.At(fields, f.Records.ID)))
var _ context.Context
var _ *database.Tx

func invalid(ctx context.Context, db *database.DB) { _, _ = selected.All(ctx, db) }
