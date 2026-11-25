package messaging

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/redis"
)

// doc-example:begin pubsub-1
func publishAndReceive(ctx context.Context, broker *pubsub.Broker, member Member) error {
	topic, err := MemberChanged.Bind(broker)
	if err != nil {
		return err
	}
	subscription, err := topic.Subscribe(ctx, member.ID)
	if err != nil {
		return err
	}
	defer subscription.Close(context.Background())

	email, err := member.AccessEmail()
	if err != nil {
		return err
	}
	subscribers, err := topic.Publish(ctx, member.ID, MemberChange{Email: email})
	if err != nil {
		return err
	}
	change, err := subscription.Receive(ctx)
	if err != nil {
		return err
	}
	_ = subscribers
	_ = change
	return err
}

// doc-example:end pubsub-1

// doc-example:begin pubsub-2
func redisBroker(client *redis.Client, namespace keyspace.Namespace) (*pubsub.Broker, error) {
	broker, err := pubsub.NewBroker(client, pubsub.DefaultConfig(namespace))
	return broker, err
}

// doc-example:end pubsub-2

// doc-example:begin pubsub-3
func superviseMember(ctx context.Context, topic pubsub.Topic[model.ID[Member], MemberChange], member Member, apply func(context.Context, MemberChange) error, reloadMember func(context.Context, model.ID[Member]) error) error {
	err := topic.Supervise(ctx, member.ID, pubsub.DefaultSupervisePolicy(),
		func(ctx context.Context, change MemberChange) error { return apply(ctx, change) },
		func(ctx context.Context, gap pubsub.Gap) error { return reloadMember(ctx, member.ID) },
	)
	if err != nil {
		return err
	}
	return err
}

// doc-example:end pubsub-3
