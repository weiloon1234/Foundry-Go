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

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(f.Records.Query())), "records_alias")
var fields = rows.TransactionScope(source)
var selected = rows.SelectTransactionValue(source, expr.AsSelected(expr.Row(expr.At(fields, orm.KeyExpr(f.Records.ID)))))
var _ context.Context
var _ *database.Tx

var _ = rows.ForUpdate(selected).Count
