package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(counts caching.ViewCounts, id model.ID[models.User]) {
	_, _ = counts.Increment(context.Background(), id, 1, cache.Forever())
}
