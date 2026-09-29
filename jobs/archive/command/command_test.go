package command_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	archivecommand "github.com/weiloon1234/Foundry-Go/jobs/archive/command"
)

func TestFailedJobsCommandValidatesBeforeServiceStartup(t *testing.T) {
	for _, args := range [][]string{
		nil, {"failed-jobs"}, {"failed-jobs", "unknown"}, {"failed-jobs", "retry"}, {"failed-jobs", "retry", "--id", "bad"},
		{"failed-jobs", "list", "--limit", "0"}, {"failed-jobs", "list", "--name", "not a name"}, {"failed-jobs", "prune"},
		{"failed-jobs", "prune", "--older-than", "1h", "--limit", "0"}, {"failed-jobs", "list", "--format", "xml"},
	} {
		if _, err := archivecommand.Parse(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	for _, args := range [][]string{{"failed-jobs", "list", "--name", "reports.export"}, {"failed-jobs", "prune", "--older-than", "720h"}} {
		if _, err := archivecommand.Parse(args, io.Discard); err != nil {
			t.Fatal("valid arguments rejected", args, err)
		}
	}
	if _, err := archivecommand.Parse([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (archivecommand.Command{}).Run(context.Background(), nil, nil, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
