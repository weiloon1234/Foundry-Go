package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
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
