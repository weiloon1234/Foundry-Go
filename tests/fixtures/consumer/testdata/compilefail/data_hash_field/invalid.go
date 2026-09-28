package invalid

import (
	"context"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(h redisdata.Profiles, id model.ID[mutatorqueries.Member], field string) {
	_, _, _ = h.Get(context.Background(), id, field)
}
