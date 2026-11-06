package telemetry_test

import (
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

func ExampleNewCollector() {
	collector, err := telemetry.NewCollector(telemetry.CollectorConfig{
		Instance: "worker-1", Release: "release-1",
		MaxSeries: 1024, MaxEvents: 1024, MaxPendingBatches: 6,
		PendingRetention: time.Hour,
	}, clock.System{})
	if err != nil {
		panic(err)
	}
	// Register these with the existing HTTP server and recorder during assembly.
	var observer http.SecurityRequestObserver = collector
	var reporter observability.Reporter = collector.Report
	fmt.Println(observer != nil, reporter != nil, !collector.RunID().IsZero())
	// Output: true true true
}
