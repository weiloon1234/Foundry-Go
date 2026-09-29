package command_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	outboxcommand "github.com/weiloon1234/Foundry-Go/outbox/command"
)

func TestOutboxCommandValidatesBeforeServiceStartup(t *testing.T) {
	for _, args := range [][]string{
		nil, {"outbox"}, {"outbox", "unknown"}, {"outbox", "requeue"},
		{"outbox", "requeue", "--all", "--kind", "job"}, {"outbox", "requeue", "--kind", "not a kind"},
		{"outbox", "requeue", "--all", "--limit", "0"}, {"outbox", "prune"}, {"outbox", "prune", "--older-than", "-1h"},
		{"outbox", "failed", "--limit", "101"}, {"outbox", "failed", "--id", "bad"}, {"outbox", "stats", "--format", "xml"},
	} {
		if _, err := outboxcommand.Parse(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	for _, args := range [][]string{
		{"outbox", "stats"}, {"outbox", "failed", "--kind", "job"}, {"outbox", "requeue", "--all"},
		{"outbox", "requeue", "--kind", "event", "--limit", "10"}, {"outbox", "prune", "--older-than", "720h"},
	} {
		if _, err := outboxcommand.Parse(args, io.Discard); err != nil {
			t.Fatal("valid arguments rejected", args, err)
		}
	}
	if _, err := outboxcommand.Parse([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (outboxcommand.Command{}).Run(context.Background(), nil, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
