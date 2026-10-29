package terminal_adjustment

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func Adjust(ctx context.Context, db database.Transactor) (int64, error) {
	return orm.UpdateAll(ctx, db, m.Orders.Query(), orm.NumberIncrement(m.Orders.TotalCents, 5, orm.AtMost(int64(100))))
}
func Nullable(ctx context.Context, db database.Transactor, amount, floor decimal.Decimal) (int64, error) {
	return orm.UpdateAll(ctx, db, m.LedgerEntries.Query(), orm.NullableNumberDecrement(m.LedgerEntries.Balance, amount, orm.AtLeast(floor)))
}
