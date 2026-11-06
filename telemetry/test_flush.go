package telemetry

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/telemetrytest"
)

// Flush captures once and drains pending batches in FIFO order for framework test
// helpers. Consumers use testkit/telemetry.Flush; the internal token prevents
// production callers from acquiring this capability. It starts no loop and works
// alongside Run or before Run starts. Close rejects new flushes and waits for
// admitted flushes before final capture and dependency cleanup.
//
// DrainTimeout bounds cooperative capture and storage work, with FlushTimeout
// still bounding each write. Callbacks retain ownership until they actually exit,
// even beyond the deadline. Unconfirmed writes follow normal retry and loss
// policies; incident evaluation and receipt reconciliation use the normal storage
// path.
func (e *Engine) Flush(ctx context.Context, token telemetrytest.Token) error {
	if !token.Valid() || e == nil || e.done == nil || ctx == nil {
		return invalid("telemetry test flush requires an initialized engine, context and test token")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Linearize admission with Close before it seals collector intake.
	e.mu.Lock()
	err := e.operations.enter()
	e.mu.Unlock()
	if err != nil {
		return err
	}
	defer e.operations.leave()
	ctx, cancel := context.WithTimeout(ctx, e.config.DrainTimeout)
	defer cancel()
	captureErr := e.capture(ctx)
	if captureErr != nil {
		e.captureFailure(captureErr)
	}
	drainErr := e.drain(ctx)
	if drainErr != nil {
		e.logFailure(drainErr)
	}
	return safeerror.Join("telemetry test flush", captureErr, drainErr, ctx.Err())
}
