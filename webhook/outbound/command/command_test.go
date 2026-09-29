package command

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
)

func TestWebhookCommandValidatesBeforeBoot(t *testing.T) {
	id := "0193a5d2-6c1e-7a52-9b7d-3f2f1c0e8a41"
	for _, args := range [][]string{
		{"webhooks", "deliveries"}, {"webhooks", "deliveries", "--state", "failed", "--endpoint", id, "--limit", "1000", "--format", "json"},
		{"webhooks", "replay", "--delivery", id}, {"webhooks", "replay", "--failed", "--endpoint", id, "--limit", "10"},
		{"webhooks", "prune", "--older-than", "720h"}, {"webhooks", "prune", "--older-than", "1h", "--limit", "10000", "--format", "json"},
	} {
		if _, err := Parse(args, io.Discard); err != nil {
			t.Fatal("valid webhook command rejected", args, err)
		}
	}
	for _, args := range [][]string{
		nil, {"webhooks"}, {"webhooks", "send"}, {"webhooks", "deliveries", "--state", "lost"},
		{"webhooks", "deliveries", "--limit", "0"}, {"webhooks", "deliveries", "--endpoint", "bad"},
		{"webhooks", "replay"}, {"webhooks", "replay", "--failed", "--delivery", id},
		{"webhooks", "replay", "--delivery", id, "--endpoint", id}, {"webhooks", "replay", "--failed", "--limit", "1001"},
		{"webhooks", "deliveries", "--format", "xml"},
		{"webhooks", "prune"}, {"webhooks", "prune", "--older-than", "30m"}, {"webhooks", "prune", "--older-than", "2h", "--limit", "10001"},
		{"webhooks", "prune", "--older-than", "2h", "--endpoint", id},
	} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid webhook command accepted", args)
		}
	}
	if _, err := Parse([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (Command{}).Run(context.Background(), outbound.Queue{}, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := Declaration(nil); err == nil {
		t.Fatal("declaration without a queue constructor accepted")
	}
}
