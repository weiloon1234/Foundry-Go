package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(c caching.Profiles, id model.ID[models.User]) { _, _, _ = c.Get(context.Background(), id) }
