package invalid

import (
	"context"
	"foundry.test/consumer/messaging"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func wrong(s *pubsub.Subscription[messaging.MemberChange]) {
	var model mutatorqueries.Member
	model, _ = s.Receive(context.Background())
	_ = model
}
