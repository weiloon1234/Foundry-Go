package pubsubstream

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func TestQueueOwnsBytesAndWrapsInFIFOOrder(t *testing.T) {
	limits := pubsub.DefaultLimits()
	limits.Messages = 2
	b, err := New(limits)
	if err != nil {
		t.Fatal(err)
	}
	channel, _ := pubsub.NewChannel(keyspace.Namespace{Application: "test", Environment: "buffer"}, "topic", 1, "key")
	for n := range 100 {
		input := []byte{byte(n)}
		if err := b.Push(pubsub.Message{Channel: channel, Data: input}); err != nil {
			t.Fatal(err)
		}
		input[0] = 255
		m, err := b.Next(t.Context())
		if err != nil || m.Data[0] != byte(n) {
			t.Fatal(m, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if b.Err() != nil {
		t.Fatal(b.Err())
	}
}
func TestByteOverflowDiscardsPendingMessagesExplicitly(t *testing.T) {
	limits := pubsub.DefaultLimits()
	limits.Bytes = 3
	limits.PayloadBytes = 3
	b, err := New(limits)
	if err != nil {
		t.Fatal(err)
	}
	channel, _ := pubsub.NewChannel(keyspace.Namespace{Application: "test", Environment: "buffer"}, "topic", 1, "key")
	if err := b.Push(pubsub.Message{Channel: channel, Data: []byte("ab")}); err != nil {
		t.Fatal(err)
	}
	if err := b.Push(pubsub.Message{Channel: channel, Data: []byte("cd")}); !errors.Is(err, pubsub.ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := b.Next(t.Context()); !errors.Is(err, pubsub.ErrOverflow) {
		t.Fatal(err)
	}
	if b.size != 0 || b.bytes != 0 {
		t.Fatal("terminal queue retained data")
	}
}
