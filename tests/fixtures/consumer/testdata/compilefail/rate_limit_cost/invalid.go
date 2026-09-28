package invalid

import (
	"context"
	"foundry.test/consumer/limiting"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(l limiting.MemberLimiter, id model.ID[mutatorqueries.Member], cost int64) {
	_, _ = l.Take(context.Background(), id, cost)
}
