// Package application runs configured application kernels with test-owned cleanup.
package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/lifecycle"
)

// kernelReadyTimeout bounds how long RunHTTP waits for the listener.
const kernelReadyTimeout = 10 * time.Second

// RunKernel runs one kernel until the test ends or stop is called. Stop runs
// once and is registered with test cleanup. It cancels Run and waits for
// StopDelay + ShutdownTimeout + two seconds. A nil or cancellation-only result
// succeeds; other errors and drain timeouts fail the test.
func RunKernel(t testing.TB, app *application.App, kernel foundation.KernelKind) (stop func()) {
	t.Helper()
	return runKernel(t, app, kernel)
}

type kernelRunner interface {
	Run(context.Context, foundation.KernelKind) error
	StopDelay() time.Duration
	ShutdownTimeout() time.Duration
}

func runKernel(t testing.TB, app kernelRunner, kernel foundation.KernelKind) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, kernel) }()
	stop = sync.OnceFunc(func() {
		cancel()
		// Run bounds its own shutdown by the application's stop delay and
		// timeout (plus a margin under a second); waiting a little longer lets
		// its precise timeout error arrive before this generic one.
		limit := app.StopDelay() + app.ShutdownTimeout() + 2*time.Second
		select {
		case err := <-done:
			if err != nil && !lifecycle.CancellationOnly(err) {
				t.Error(err)
			}
		case <-time.After(limit):
			t.Errorf("%s kernel did not drain within %s", kernel, limit)
		}
	})
	t.Cleanup(stop)
	return stop
}

// RunHTTP runs the HTTP kernel of app for the rest of the test and returns its
// base URL, such as "http://127.0.0.1:52011", once it accepts requests. A kernel
// that fails to start fails the test at once, with its own error.
func RunHTTP(t testing.TB, app *application.App) string {
	t.Helper()
	stop := RunKernel(t, app, foundation.HTTP)
	ready, cancel := context.WithTimeout(t.Context(), kernelReadyTimeout)
	defer cancel()
	// An application that fails to boot is done without ever being ready.
	go func() {
		select {
		case <-app.Done():
			cancel()
		case <-ready.Done():
		}
	}()
	address, err := app.HTTPReady(ready)
	if err != nil {
		stop() // reports why Run returned
		t.Fatalf("HTTP kernel is not ready: %v", err)
	}
	return "http://" + address
}
