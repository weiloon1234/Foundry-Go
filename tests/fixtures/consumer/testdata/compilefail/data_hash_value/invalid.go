package invalid

import (
	"context"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(h redisdata.Profiles, id model.ID[mutatorqueries.Member]) {
	_, _ = h.Set(context.Background(), id, redisdata.DisplayProfile, "raw")
}
