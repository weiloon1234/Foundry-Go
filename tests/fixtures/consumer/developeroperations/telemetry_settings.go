package developeroperations

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// doc-example:begin configure-telemetry
func BuildTelemetry(ctx context.Context, settings application.Settings) (*application.App, error) {
	settings.Features.Observability.Enabled = true
	settings.Features.Telemetry = application.TelemetrySettings{
		Enabled: true, Schema: "operations",
		Collector: telemetry.CollectorConfig{
			Instance: "api-1", Release: "release-1",
			MaxSeries: 512, MaxEvents: 512, MaxPendingBatches: 12,
			PendingRetention: 5 * time.Minute,
		},
		Config: telemetry.Config{
			FlushInterval: 10 * time.Second,
			FlushTimeout:  5 * time.Second,
			DrainTimeout:  3 * time.Second,
			Retention: telemetry.RetentionPolicy{
				Minute: 24 * time.Hour, Hour: 7 * 24 * time.Hour,
				Issue: 7 * 24 * time.Hour, Security: 24 * time.Hour,
				MaxRuns: 1000, MaxReceiptRows: 100000,
				MaxMetricRows: 100000, MaxIssueRows: 10000, MaxEventRows: 10000,
				PruneInterval: time.Minute,
			},
			Reads: telemetry.ReadLimits{
				MaxConcurrent: 4, MaxOverviewRuns: 100, MaxOverviewAlerts: 100,
				MaxTrafficRows: 100000, MaxTrafficGroups: 512,
				MaxPage: 100, MaxPageSize: 30, Timeout: time.Second,
			},
		},
	}
	return application.New(settings).Build(ctx)
}

// doc-example:end configure-telemetry
