package telemetry

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (e *Engine) capture(ctx context.Context) error {
	return callback.Isolated("telemetry capture", func() error {
		health := Health{}
		if e.deps.Readiness != nil {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			report, err := e.deps.Readiness.Readiness(probe)
			cancel()
			health.State, health.Mode = report.State, report.Mode
			if err == nil {
				health.Ready = value.Of(report.Ready)
			}
			for _, result := range report.Dependencies.Results {
				health.Dependencies = append(health.Dependencies, DependencyHealth{ID: result.ID, State: result.State})
			}
		}
		routing := e.deps.DB.RoutingStats()
		usage := poolUsage(routing.Primary)
		if read, ok := routing.Read.Get(); ok {
			usage = max(usage, poolUsage(read))
		}
		return e.collector.captureRuntime(e.deps.Recorder, health, usage)
	})
}

func poolUsage(pool database.PoolStats) float64 {
	if pool.MaxOpen <= 0 {
		return 0
	}
	return float64(pool.InUse) / float64(pool.MaxOpen)
}
