package health_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/health"
)

func TestReadinessOwnsDeclarationsAndHidesDependencyErrors(t *testing.T) {
	var calls atomic.Int64
	probes := []health.Probe{{ID: "database.primary", Check: func(context.Context) error { calls.Add(1); return nil }}, {ID: "database.read", Check: func(context.Context) error { return errors.New("credential-private") }}}
	r, err := health.NewRegistry(health.DefaultConfig(), probes...)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("construction ran readiness work")
	}
	probes[0].Check = func(context.Context) error { panic("modified caller callback") }
	report, err := r.Check(t.Context())
	if err != nil || report.Ready || len(report.Results) != 2 || report.Results[0].State != health.Up || report.Results[1].State != health.Down || calls.Load() != 1 {
		t.Fatal("dependency readiness was not preserved", err, report)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "credential-private") {
		t.Fatal("readiness leaked dependency error")
	}
	ids := r.Describe()
	ids[0] = "changed"
	if r.Describe()[0] != "database.primary" {
		t.Fatal("descriptor aliases registry")
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	closed, err := r.Check(t.Context())
	if err != nil || closed.Ready || closed.Results[0].State != health.Closed || calls.Load() != 1 {
		t.Fatal("closed registry ran a callback", err)
	}
}

func TestReadinessBoundsIgnoredCancellationAndRetainsShutdownOwnership(t *testing.T) {
	config := health.DefaultConfig()
	config.MaxConcurrent, config.CheckTimeout, config.ProbeTimeout = 1, 80*time.Millisecond, 20*time.Millisecond
	release := make(chan struct{})
	var calls atomic.Int64
	r, err := health.NewRegistry(config, health.Probe{ID: "blocked", Check: func(context.Context) error { calls.Add(1); <-release; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := r.Close(ctx); err != nil {
			t.Error("released callback did not finish shutdown", err)
		}
	})
	for range 3 {
		report, err := r.Check(t.Context())
		if err != nil || report.Ready || report.Results[0].State != health.TimedOut {
			t.Fatal("probe timeout did not bound caller", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("timed-out probes escaped shared concurrency bound")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("close forgot blocked callback", err)
	}
	select {
	case <-r.Done():
		t.Fatal("done closed before callback exit")
	default:
	}
}

func TestReadinessIsolatesPanicAndGoexit(t *testing.T) {
	for _, check := range []func(context.Context) error{
		func(context.Context) error { panic("private panic") },
		func(context.Context) error { runtime.Goexit(); return nil },
	} {
		r, err := health.NewRegistry(health.DefaultConfig(), health.Probe{ID: "callback", Check: check})
		if err != nil {
			t.Fatal(err)
		}
		report, err := r.Check(t.Context())
		if err != nil || report.Ready || report.Results[0].State != health.CallbackFailed {
			t.Fatal("abnormal callback escaped isolation", err)
		}
		if err := r.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadinessRejectsInvalidOrDuplicateDeclarations(t *testing.T) {
	probe := health.Probe{ID: "dependency", Check: func(context.Context) error { return nil }}
	if _, err := health.NewRegistry(health.DefaultConfig(), probe, probe); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate accepted", err)
	}
	if _, err := health.NewRegistry(health.Config{}, probe); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded readiness accepted", err)
	}
	if _, err := health.NewRegistry(health.DefaultConfig(), health.Probe{ID: "invalid space", Check: probe.Check}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid ID accepted", err)
	}
	r, err := health.NewRegistry(health.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if report, err := r.Check(t.Context()); err != nil || !report.Ready || len(report.Results) != 0 {
		t.Fatal("empty dependency set became unhealthy", err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := r.Check(t.Context()); err != nil || report.Ready {
		t.Fatal("closed empty registry became healthy", err)
	}
}
