package routestyle

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestDeadlineDoesNotWaitForAnalysis(t *testing.T) {
	// Virtual time: the deadline and the wait below cannot race a loaded runner.
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		release, finished, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
		defer func() { close(release); <-finished }()
		result := make(chan error, 1)
		go func() {
			_, err := waitForCheck(ctx, func() ([]Diagnostic, error) {
				close(started)
				<-release // Models a parser/type checker that cannot observe cancellation.
				close(finished)
				return nil, nil
			})
			result <- err
		}()
		<-started
		select {
		case err := <-result:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("waited for uncancellable analysis")
		}
	})
}
