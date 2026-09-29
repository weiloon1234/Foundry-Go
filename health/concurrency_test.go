package health_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/health"
)

func TestReadinessRunsProbesConcurrentlyInDeclarationOrder(t *testing.T) {
	var started atomic.Int32
	barrier := make(chan struct{})
	wait := func(ctx context.Context) error {
		if started.Add(1) == 3 {
			close(barrier)
		}
		select {
		case <-barrier:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	config := health.DefaultConfig()
	config.CheckTimeout, config.ProbeTimeout = 2*time.Second, time.Second
	r, err := health.NewRegistry(config, health.Probe{ID: "first", Check: wait}, health.Probe{ID: "second", Check: wait}, health.Probe{ID: "third", Check: wait})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	report, err := r.Check(t.Context())
	if err != nil || !report.Ready || report.Results[0].ID != "first" || report.Results[1].ID != "second" || report.Results[2].ID != "third" {
		t.Fatal("probes were serialized or reordered", err, report)
	}
}

func TestReadinessTimesOutSlowProbeWithoutDelayingOthers(t *testing.T) {
	config := health.DefaultConfig()
	config.CheckTimeout, config.ProbeTimeout = time.Second, 100*time.Millisecond
	release := make(chan struct{})
	r, err := health.NewRegistry(config,
		health.Probe{ID: "slow", Check: func(ctx context.Context) error { <-ctx.Done(); <-release; return nil }},
		health.Probe{ID: "fast", Check: func(context.Context) error { return nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(release)
		if err := r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	report, err := r.Check(t.Context())
	if err != nil || report.Ready || report.Results[0].State != health.TimedOut || report.Results[1].State != health.Up || report.Duration >= config.CheckTimeout {
		t.Fatal("slow probe was not bounded independently", err, report)
	}
}
