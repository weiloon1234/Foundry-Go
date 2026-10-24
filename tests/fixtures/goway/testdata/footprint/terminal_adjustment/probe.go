package terminal_adjustment

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func Adjust(ctx context.Context, db database.Transactor) (int64, error) {
	return m.Orders.Query().UpdateAll(ctx, db, m.Orders.TotalCents.Increment(5, orm.AtMost(int64(100))))
}
func Nullable(ctx context.Context, db database.Transactor, amount, floor decimal.Decimal) (int64, error) {
	return m.LedgerEntries.Query().UpdateAll(ctx, db, m.LedgerEntries.Balance.Decrement(amount, orm.AtLeast(floor)))
}
