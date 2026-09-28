package foundation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// WithObservability gives this application ownership of one fresh recorder.
// Build remains pure; Start runs reporters and Shutdown drains them after all
// other resources. Do not reuse a recorder across apps or run/close it manually
// after handing it to the builder. Its maintenance gate is shared by kernels.
func WithObservability(recorder *observability.Recorder) Option {
	return func(s *settings) error {
		if recorder == nil || s.observability != nil {
			return fault.New(fault.Invalid, "application requires one non-nil observation recorder")
		}
		if err := recorder.Claim(); err != nil {
			return err
		}
		s.observability = recorder
		return nil
	}
}

func (a *App) Observability() *observability.Recorder     { return a.settings.observability }
func (r *Runtime) Observability() *observability.Recorder { return r.core.observability }

// Foundation IDs predate semantic metric names and may contain Unicode or
// exceed their limit when qualified. Preserve every declared resource without
// widening labels or silently dropping its lifecycle observation.
func observationName(declared string) observability.Name {
	if identifier.Semantic(declared) {
		return observability.Name(declared)
	}
	digest := sha256.Sum256([]byte(declared))
	return observability.Name("declared." + hex.EncodeToString(digest[:]))
}

func (r *runtimeState) startObservability() error {
	if r.observability == nil {
		return nil
	}
	finished := make(chan error, 1)
	// Install ownership before starting a goroutine. This first cleanup runs
	// last, allowing resource shutdown failures to reach the reporter queue.
	r.mu.Lock()
	r.cleanups = append(r.cleanups, cleanup{name: "observability reporters", close: func(ctx context.Context) error {
		err := r.observability.Close(ctx)
		<-r.observability.Done()
		return errors.Join(err, <-finished)
	}})
	r.mu.Unlock()
	go func() {
		err := r.observability.Run(context.Background())
		finished <- err
		if err != nil {
			r.cancel(err)
		}
	}()
	return r.observability.Ready(r.ctx)
}

func (r *runtimeState) observe(ctx context.Context, operation observability.Operation, run func(context.Context) error) error {
	return observability.Observe(observability.WithContext(ctx, r.observability), operation, run)
}

func (a *App) stopPreparedObservability(recorder *observability.Recorder, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := recorder.Close(ctx)
	<-recorder.Done()
	cancel()
	a.mu.Lock()
	a.shutdownErr = err
	a.state = Stopped
	close(a.done)
	a.mu.Unlock()
}
