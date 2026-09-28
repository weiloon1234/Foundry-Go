package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/model"
	"time"
)

func wrong(c caching.Profiles, id model.ID[mutatorqueries.Member]) {
	_, _ = c.Expire(context.Background(), id, time.Minute)
}
