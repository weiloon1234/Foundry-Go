package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(c caching.Profiles, ids []model.ID[models.User]) {
	_, _ = c.ForgetMany(context.Background(), ids...)
}
