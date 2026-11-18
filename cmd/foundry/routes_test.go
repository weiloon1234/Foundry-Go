package main

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/routestyle"
)

func TestRoutesDeadlineExitCode(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	err := runRoutes(ctx, []string{"check", "--dir", t.TempDir()}, io.Discard, io.Discard)
	if cli.Status(err) != cli.TimedOut {
		t.Fatalf("deadline status %d: %v", cli.Status(err), err)
	}
}

func TestRoutesDeadlineDoesNotWaitForAnalysis(t *testing.T) {
	// Virtual time: the deadline and the wait below cannot race a loaded runner.
	synctest.Test(t, func(t *testing.T) {
		release, finished, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
		defer func() { close(release); <-finished }()
		result := make(chan error, 1)
		go func() {
			result <- runRoutesWithCheck(t.Context(), []string{"check", "--timeout", "50ms"}, io.Discard, io.Discard,
				func(context.Context, string, []string) ([]routestyle.Diagnostic, error) {
					close(started)
					<-release // Models a parser/type checker that cannot observe cancellation.
					close(finished)
					return nil, nil
				})
		}()
		<-started
		select {
		case err := <-result:
			if cli.Status(err) != cli.TimedOut {
				t.Fatalf("deadline status %d: %v", cli.Status(err), err)
			}
		case <-time.After(time.Second):
			t.Fatal("CLI waited for uncancellable analysis")
		}
	})
}
