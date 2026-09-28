package pubsubstream

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func FuzzBoundedDeliveryQueue(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{2, 2, 2, 2, 2})
	f.Add([]byte{1, 0, 1, 0})
	channel, _ := pubsub.NewChannel(keyspace.Namespace{Application: "fuzz", Environment: "buffer"}, "messages", 1, "key")
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			return
		}
		limits := pubsub.Limits{Messages: 4, Bytes: 12, PayloadBytes: 6, Channels: 1}
		buffer, err := New(limits)
		if err != nil {
			t.Fatal(err)
		}
		reference := [][]byte{}
		used := 0
		terminal := false
		for _, op := range operations {
			if op%2 == 0 {
				payload := make([]byte, int((op/2)%6)+1)
				for i := range payload {
					payload[i] = op
				}
				overflow := len(reference) == limits.Messages || len(payload) > limits.Bytes-used
				err := buffer.Push(pubsub.Message{Channel: channel, Data: payload})
				if terminal || overflow {
					if !errors.Is(err, pubsub.ErrOverflow) {
						t.Fatal(err)
					}
					terminal = true
					reference = nil
					used = 0
				} else {
					if err != nil {
						t.Fatal(err)
					}
					reference = append(reference, payload)
					used += len(payload)
				}
			} else {
				ctx := t.Context()
				cancel := func() {}
				if len(reference) == 0 {
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				message, err := buffer.Next(ctx)
				cancel()
				if terminal {
					if !errors.Is(err, pubsub.ErrOverflow) {
						t.Fatal(err)
					}
				} else if len(reference) == 0 {
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				} else {
					if err != nil || string(message.Data) != string(reference[0]) {
						t.Fatal(message, err)
					}
					used -= len(reference[0])
					reference = reference[1:]
				}
			}
			if buffer.size != len(reference) || buffer.bytes != used || buffer.size > limits.Messages || buffer.bytes > limits.Bytes {
				t.Fatal("queue exceeded model bounds")
			}
		}
	})
}
