// Package messaging verifies ephemeral typed publication through Foundry.
package messaging

import (
	"context"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// MemberChange is a declared transport snapshot. It does not serialize the model.
type MemberChange struct{ Email mutatorqueries.DisplayEmail }

var MemberChanged = pubsub.Define[model.ID[mutatorqueries.Member], MemberChange]("member-changed", 1, keyspace.TextKeys[model.ID[mutatorqueries.Member]]())

type MemberChanges = pubsub.Topic[model.ID[mutatorqueries.Member], MemberChange]

func Bind(broker *pubsub.Broker) (MemberChanges, error) { return MemberChanged.Bind(broker) }

// PublishMember deliberately selects the getter while preserving stored identity.
func PublishMember(ctx context.Context, topic MemberChanges, member mutatorqueries.Member) (uint64, error) {
	email, err := member.AccessEmail()
	if err != nil {
		return 0, err
	}
	return topic.Publish(ctx, member.ID, MemberChange{Email: email})
}
func SubscribeMember(ctx context.Context, topic MemberChanges, id model.ID[mutatorqueries.Member]) (*pubsub.Subscription[MemberChange], error) {
	return topic.Subscribe(ctx, id)
}
func ReceiveMember(ctx context.Context, subscription *pubsub.Subscription[MemberChange]) (MemberChange, error) {
	return subscription.Receive(ctx)
}
