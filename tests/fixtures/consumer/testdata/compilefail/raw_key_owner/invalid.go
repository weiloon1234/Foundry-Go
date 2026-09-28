package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/rediscommands"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(keys rediscommands.MemberKeys, id model.ID[models.User]) {
	_, _ = keys.For(context.Background(), id)
}
