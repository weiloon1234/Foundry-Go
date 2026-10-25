package fail

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.EachRead(context.Background(), nil, rows.Read(rows.FromModel(models.Orders.Query())), func(int64) error { return nil })
