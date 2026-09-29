// Package diagnostics exposes bounded, payload-free operational views through
// ordinary authenticated Foundry routes. It owns no listeners or dependencies.
package diagnostics

import (
	"context"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// Config bounds diagnostics requests. Profiling additionally enables the
// Profile endpoint; it stays disabled unless explicitly configured.
type Config struct {
	MaxConcurrent int
	Profiling     bool
}

func DefaultConfig() Config { return Config{MaxConcurrent: 8} }
func (c Config) Validate() error {
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 64 {
		return fault.New(fault.Invalid, "invalid diagnostics concurrency limit")
	}
	return nil
}

// Runtime borrows its application recorder and readiness registry. Their owners
// must outlive admitted diagnostics requests. Do not copy a Runtime.
type Runtime struct {
	mu        sync.Mutex
	state     func() foundation.State
	recorder  *observability.Recorder
	gate      *maintenance.Gate
	probes    *health.Registry
	slots     chan struct{}
	profiling bool
	profile   chan struct{}
}

func prepare(config Config, probes *health.Registry) (*Runtime, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if probes == nil {
		return nil, fault.New(fault.Invalid, "diagnostics requires an explicit readiness registry")
	}
	return &Runtime{probes: probes, slots: make(chan struct{}, config.MaxConcurrent), profiling: config.Profiling, profile: make(chan struct{}, 1)}, nil
}

// New binds an already built app. Module supports ordinary service resolution
// while the app is still being assembled, binding its lifecycle at boot.
func New(app *foundation.App, probes *health.Registry, config Config) (*Runtime, error) {
	if app == nil {
		return nil, fault.New(fault.Invalid, "diagnostics requires an application")
	}
	runtime, err := prepare(config, probes)
	if err != nil {
		return nil, err
	}
	runtime.state, runtime.recorder, runtime.gate = app.State, app.Observability(), app.Maintenance()
	return runtime, nil
}

func (r *Runtime) source() (foundation.State, *observability.Recorder, *maintenance.Gate) {
	r.mu.Lock()
	state, recorder, gate := r.state, r.recorder, r.gate
	r.mu.Unlock()
	if gate == nil {
		gate = recorder.Gate()
	}
	if state == nil {
		return foundation.Prepared, recorder, gate
	}
	return state(), recorder, gate
}

type LivenessReport struct {
	Live  bool             `json:"live"`
	State foundation.State `json:"state"`
}
type ReadinessReport struct {
	Ready        bool             `json:"ready"`
	State        foundation.State `json:"state"`
	Mode         maintenance.Mode `json:"mode"`
	Dependencies health.Report    `json:"dependencies"`
}
type Snapshot struct {
	State        foundation.State       `json:"state"`
	Observations observability.Snapshot `json:"observations"`
}

// Liveness reads lifecycle state only. Dependency failures and reversible
// maintenance never cause a restart signal. A stopped app is no longer live.
func (r *Runtime) Liveness() LivenessReport {
	state, _, _ := r.source()
	return LivenessReport{Live: state != foundation.Stopped, State: state}
}

// Readiness checks dependencies only while the app can admit new work. A
// shutdown/maintenance transition during the check is reflected in the result.
func (r *Runtime) Readiness(ctx context.Context) (ReadinessReport, error) {
	state, _, gate := r.source()
	report := ReadinessReport{State: state, Mode: gate.Mode(), Dependencies: health.Report{Results: []health.Result{}}}
	if ctx == nil {
		return report, fault.New(fault.Invalid, "readiness requires a context")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if state != foundation.Running || report.Mode != maintenance.Serving {
		return report, nil
	}
	dependencies, err := r.probes.Check(ctx)
	report.Dependencies = dependencies
	report.State, _, gate = r.source()
	report.Mode = gate.Mode()
	report.Ready = err == nil && dependencies.Ready && report.State == foundation.Running && report.Mode == maintenance.Serving
	return report, err
}

func (r *Runtime) Snapshot() Snapshot {
	state, recorder, gate := r.source()
	observations := recorder.Snapshot()
	// Report the application's gate even when observability is disabled.
	observations.Mode = gate.Mode()
	return Snapshot{State: state, Observations: observations}
}
