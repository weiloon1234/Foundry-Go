package fail

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.EachRead(context.Background(), nil, rows.ReadScalar(rows.SelectScalar(rows.From(models.Orders.Query()), expr.Select(expr.Value(models.Orders.TotalCents)))), func(models.Order) error { return nil })
