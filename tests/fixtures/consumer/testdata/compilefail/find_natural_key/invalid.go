package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/consumer/models"
)

var invalid, _ = orm.Find(context.Background(), nil, models.Countries.Table, models.StatusActive)
