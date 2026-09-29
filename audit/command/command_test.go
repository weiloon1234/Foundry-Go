package command_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/command"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestAuditPruneParsingIsBoundedAndPure(t *testing.T) {
	var help bytes.Buffer
	if _, err := command.Parse([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), "audit prune") {
		t.Fatal("help did not describe the command", err)
	}
	for _, args := range [][]string{
		{"audit", "prune"},
		{"audit", "prune", "--before", "2026-01-01T00:00:00Z", "--batch", "10", "--area", "accounts", "--format", "json"},
		{"audit", "prune", "--apply"},
	} {
		if _, err := command.Parse(args, &help); err != nil {
			t.Fatal("valid invocation rejected", args, err)
		}
	}
	for _, args := range [][]string{
		{"audit"}, {"audit", "purge"}, {"audit", "prune", "--batch", "0"}, {"audit", "prune", "--batch", "100000"},
		{"audit", "prune", "--before", "yesterday"}, {"audit", "prune", "--area", "bad area"}, {"audit", "prune", "extra"},
		{"audit", "prune", "--format", "xml"}, {"audit", "prune", "--apply=maybe"},
	} {
		if _, err := command.Parse(args, &help); err == nil {
			t.Fatal("invalid invocation accepted", args)
		}
	}
	if err := (command.Command{}).Run(t.Context(), nil, clock.System{}, &help); !errors.Is(err, fault.Invalid) {
		t.Fatal("unparsed command ran", err)
	}
	if _, err := command.Declaration(nil, clock.System{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("declaration accepted a missing scope constructor", err)
	}
}
