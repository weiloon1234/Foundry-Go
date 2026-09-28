package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/rediscommands"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(k rediscommands.MemberKeys, id model.ID[models.User]) {
	_, _ = k.Expire(context.Background(), id, cache.Forever())
}
