package memory_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/pubsubtest"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
)

func TestSharedContract(t *testing.T) {
	pubsubtest.Run(t, func(t *testing.T) (pubsub.Backend, func(string) pubsub.Channel) {
		b, err := memory.New(10)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		return b, func(text string) pubsub.Channel {
			c, err := pubsub.NewChannel(keyspace.Namespace{Application: "test", Environment: "memory"}, "shared", 1, text)
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
	})
}
