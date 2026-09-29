package application

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/http"
)

const ProbeProvider foundation.ProviderID = "foundry.application.probes"

// ProbeKey resolves the diagnostics runtime backing the public probe routes.
var ProbeKey = foundation.NewKey[*diagnostics.Runtime](string(ProbeProvider))

var probeHealthKey = foundation.NewKey[*health.Registry](string(ProbeProvider) + ".health")

// ProbeSettings mount unauthenticated probe routes on the application listener.
// Liveness performs no dependency checks and reports only that the process is
// live. Readiness reuses diagnostics readiness (lifecycle, maintenance and the
// configured health probes) and reports only ready/unavailable. Both paths stay
// reachable while maintenance is paused. Authenticated diagnostics are separate.
type ProbeSettings struct {
	Liveness      bool
	LivenessPath  string
	Readiness     bool
	ReadinessPath string
	// ReadinessCache is how long public readiness reuses one dependency check
	// (100ms to one minute); lifecycle and maintenance state are read live.
	ReadinessCache time.Duration
}

func DefaultProbeSettings() ProbeSettings {
	return ProbeSettings{LivenessPath: "/up", ReadinessPath: "/ready", ReadinessCache: diagnostics.DefaultReadinessCache}
}

func (s ProbeSettings) paths() []string {
	var paths []string
	if s.Liveness {
		paths = append(paths, s.LivenessPath)
	}
	if s.Readiness {
		paths = append(paths, s.ReadinessPath)
	}
	return paths
}

func (s ProbeSettings) validate() error {
	paths := s.paths()
	for _, path := range paths {
		if canonical, err := http.StaticPath(path).URL(http.NoPath{}); err != nil || canonical != path {
			return fault.New(fault.Invalid, "probe paths must be canonical static paths")
		}
	}
	if len(paths) == 2 && paths[0] == paths[1] {
		return fault.New(fault.Duplicate, "liveness and readiness probes need distinct paths")
	}
	if s.Readiness && (s.ReadinessCache < 100*time.Millisecond || s.ReadinessCache > time.Minute) {
		return fault.New(fault.Invalid, "public readiness cache must be from 100ms to one minute")
	}
	return nil
}

// registerProbes registers the probe runtime and returns its route constructor.
// Readiness uses the configured health registry, or an empty one when health
// is disabled (lifecycle and maintenance only).
func registerProbes(builder *foundation.Builder, s Settings) (Routes, error) {
	probes := s.HTTP.Probes
	if !s.HTTP.Enabled || (!probes.Liveness && !probes.Readiness) {
		return nil, nil
	}
	if err := probes.validate(); err != nil {
		return nil, err
	}
	registry, requires := HealthKey, []foundation.ProviderID{HealthProvider}
	if !s.Features.Health.Enabled {
		registry, requires = probeHealthKey, []foundation.ProviderID{ProbeProvider + ".health"}
		builder.Register(health.Module(ProbeProvider+".health", probeHealthKey, health.DefaultConfig(), nil, func(foundation.Resolver) ([]health.Probe, error) { return nil, nil }))
	}
	builder.Register(diagnostics.Module(ProbeProvider, ProbeKey, registry, diagnostics.DefaultConfig(), requires))
	return func(services Services) ([]http.RouteRegistration, error) {
		runtime, err := Resolve(services, ProbeKey)
		if err != nil {
			return nil, err
		}
		var routes []http.RouteRegistration
		if probes.Liveness {
			routes = append(routes, diagnostics.PublicProbe(runtime, "foundry.probes.liveness", probes.LivenessPath, diagnostics.Liveness))
		}
		if probes.Readiness {
			routes = append(routes, diagnostics.PublicProbe(runtime, "foundry.probes.readiness", probes.ReadinessPath, diagnostics.Readiness, diagnostics.ReadinessCache(probes.ReadinessCache)))
		}
		return routes, nil
	}, nil
}
