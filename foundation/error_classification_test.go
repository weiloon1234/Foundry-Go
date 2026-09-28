package foundation_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

type lifecycleClassificationError struct{ inspect func() }

func (lifecycleClassificationError) Error() string   { return "lifecycle callback failure" }
func (e lifecycleClassificationError) Unwrap() error { e.inspect(); return nil }

func TestStartupErrorInspectionCannotStrandShutdownOrHoldApplicationMutex(t *testing.T) {
	for _, exit := range []bool{false, true} {
		var app *foundation.App
		var closed atomic.Bool
		failure := lifecycleClassificationError{inspect: func() {
			_ = app.State() // Inspection must not run under the application's mutex.
			if exit {
				runtime.Goexit()
			}
			panic("private callback state")
		}}
		app = build(t, foundation.Module{Name: "inspection", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			if err := r.OnShutdown("owned", func(context.Context) error { closed.Store(true); return nil }); err != nil {
				return err
			}
			return failure
		}})
		if err := app.Start(t.Context()); err == nil {
			t.Fatal("startup unexpectedly passed")
		}
		wait, cancel := context.WithTimeout(t.Context(), time.Second)
		err := app.Shutdown(wait)
		cancel()
		if err == nil || app.State() != foundation.Stopped || !closed.Load() {
			t.Fatal("startup inspection stranded cleanup", err)
		}
	}
}
