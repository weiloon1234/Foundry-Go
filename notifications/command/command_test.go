package command_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	notificationcommand "github.com/weiloon1234/Foundry-Go/notifications/command"
)

func TestNotificationCommandValidatesBeforeServiceStartup(t *testing.T) {
	id := strings.Repeat("ab", 32)
	for _, args := range [][]string{
		nil, {"notifications"}, {"notifications", "unknown"}, {"notifications", "deliveries"},
		{"notifications", "deliveries", "--state", "delivered"}, {"notifications", "deliveries", "--state", "running", "--limit", "101"},
		{"notifications", "resolve", "--id", id, "--expect", "running"}, {"notifications", "resolve", "--id", id, "--expect", "pending", "--as", "resend"},
		{"notifications", "resolve", "--id", "bad", "--expect", "running", "--as", "resend"}, {"notifications", "resolve", "--id", id, "--expect", "uncertain", "--as", "retry"},
		{"notifications", "deliver"}, {"notifications", "deliver", "--notification", "bad"},
	} {
		if _, err := notificationcommand.Parse(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	for _, args := range [][]string{
		{"notifications", "deliveries", "--state", "uncertain"},
		{"notifications", "resolve", "--id", id, "--expect", "uncertain", "--as", "delivered", "--format", "json"},
	} {
		if _, err := notificationcommand.Parse(args, io.Discard); err != nil {
			t.Fatal("valid arguments rejected", args, err)
		}
	}
	if _, err := notificationcommand.Parse([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (notificationcommand.Command{}).Run(context.Background(), nil, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
