package invalid

import (
	"context"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(s redisdata.MemberGroups, id model.ID[mutatorqueries.Member]) {
	_, _ = s.Add(context.Background(), id, id)
}
