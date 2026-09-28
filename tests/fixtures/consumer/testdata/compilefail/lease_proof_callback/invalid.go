package invalid

import (
	"context"
	"foundry.test/consumer/coordination"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
	"time"
)

func wrong(l coordination.MemberLeases, id model.ID[mutatorqueries.Member]) {
	_, _ = l.WithProof(context.Background(), id, time.Second, 0, func(context.Context, lease.Owner) error { return nil })
}
