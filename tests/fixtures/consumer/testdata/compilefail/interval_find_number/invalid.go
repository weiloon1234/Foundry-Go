package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/consumer/intervalqueries"
)

func invalid() { _, _ = orm.Find(context.Background(), nil, intervalqueries.Labels.Table, 1) }
