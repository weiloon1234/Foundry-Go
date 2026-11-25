package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/modelcheck"
)

func TestModelsDeadlineExitCode(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	err := runModels(ctx, []string{"check", "--dir", t.TempDir()}, io.Discard, io.Discard)
	if cli.Status(err) != cli.TimedOut {
		t.Fatalf("deadline status %d: %v", cli.Status(err), err)
	}
}

func TestModelsOnlyCheck(t *testing.T) {
	var out bytes.Buffer
	if err := runModels(t.Context(), []string{"--help"}, &out, io.Discard); cli.Status(err) != cli.Success {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "models check") || strings.Contains(out.String(), "convert") {
		t.Fatalf("unexpected models help: %s", out.String())
	}
	if err := runModels(t.Context(), []string{"convert"}, io.Discard, io.Discard); cli.Status(err) != cli.InvalidUsage {
		t.Fatalf("removed subcommand status %d: %v", cli.Status(err), err)
	}
}

// TestModelsSuccessLineSaysWhatItCompared holds the clean line to the counts: it
// names how many enum columns were compared, never claims a column that has no
// CHECK constraint was.
func TestModelsSuccessLineSaysWhatItCompared(t *testing.T) {
	for _, tc := range []struct {
		report modelcheck.Report
		want   string
	}{
		{modelcheck.Report{}, "there is no enum column to compare,"},
		{modelcheck.Report{EnumColumns: 5, EnumCompared: 3}, "3 of 5 enum columns were compared with their CHECK constraints and none rejects a case (the rest have none; foreign keys, triggers and tests across columns are not read),"},
		{modelcheck.Report{EnumColumns: 2}, "0 of 2 enum columns were compared with their CHECK constraints and none rejects a case (the rest have none; foreign keys, triggers and tests across columns are not read),"},
	} {
		if got := enumSummary(tc.report); got != tc.want {
			t.Fatalf("%+v\n got %s\nwant %s", tc.report, got, tc.want)
		}
	}
}
