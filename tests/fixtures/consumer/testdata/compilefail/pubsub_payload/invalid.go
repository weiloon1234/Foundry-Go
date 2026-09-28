package invalid

import (
	"context"
	"foundry.test/consumer/messaging"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(t messaging.MemberChanges, id model.ID[mutatorqueries.Member]) {
	_, _ = t.Publish(context.Background(), id, mutatorqueries.Member{})
}
