package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var errRunCapacity = safeerror.NewFault(fault.Overloaded, "telemetry run capacity reached")

const runCapacityWarningInterval = time.Hour

func (e *Engine) suppressCapacityWarning(err error) bool {
	capacity := false
	if callback.Isolated("telemetry capacity diagnostic", func() error {
		capacity = errorgraph.Is(err, errRunCapacity)
		return nil
	}) != nil || !capacity {
		return false
	}
	now := e.collector.elapsed()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capacityRefusals = saturatingAdd(e.capacityRefusals, 1)
	if e.capacityRefusals > 1 && now.Sub(e.capacityWarningAt) < runCapacityWarningInterval {
		return true
	}
	e.capacityWarningAt = now
	return false
}

// Only a confirmed batch commit or receipt reconciliation announces recovery.
// An empty queue, expired batch, successful prune or unknown commit cannot do so.
func (e *Engine) logCapacityRecovery() {
	e.mu.Lock()
	refusals := e.capacityRefusals
	e.capacityRefusals = 0
	e.capacityWarningAt = time.Time{}
	e.mu.Unlock()
	if refusals == 0 || e.deps.Logger == nil {
		return
	}
	_ = callback.Isolated("telemetry logger", func() error {
		e.deps.Logger.LogAttrs(context.Background(), slog.LevelInfo, "telemetry run capacity recovered", slog.Uint64("refusals", refusals))
		return nil
	})
}
