package main

import (
	"context"
	"io"
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
