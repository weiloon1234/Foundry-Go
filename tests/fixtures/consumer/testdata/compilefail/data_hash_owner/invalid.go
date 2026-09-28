package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(h redisdata.Profiles, id model.ID[models.User]) {
	_, _, _ = h.Get(context.Background(), id, redisdata.DisplayProfile)
}
