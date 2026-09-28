package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(s redisdata.MemberGroups, id model.ID[models.User]) {
	_, _ = s.Expire(context.Background(), id, cache.Forever())
}
