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

var source = rows.AsTransaction[a](rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(f.Records.Query()))), "claim")
var selected = rows.SelectTransactionRecord(source, rows.TransactionScope(source))
var _ context.Context
var _ *database.Tx

var other = rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(f.Records.Query())), "other")
var _ = rows.InnerJoin(source, other, rows.OnEqual(expr.At(rows.TransactionScope(source), orm.KeyExpr(f.Records.ID)), expr.At(rows.TransactionScope(other), orm.KeyExpr(f.Records.ID))))
