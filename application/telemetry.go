package application

import (
	"context"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// TelemetryProvider owns configured telemetry after its borrowed dependencies boot.
const TelemetryProvider foundation.ProviderID = "foundry.application.telemetry"

// TelemetryKey identifies the configured telemetry engine.
var TelemetryKey = foundation.NewKey[*telemetry.Engine](string(TelemetryProvider))

// TelemetryReadiness borrows an explicitly registered diagnostics runtime. Its
// actual provider becomes a boot dependency; omission records readiness as
// unavailable. The declaration creates no diagnostics service of its own.
func (b *Builder) TelemetryReadiness(key foundation.Key[*diagnostics.Runtime]) *Builder {
	b.mutate(func() {
		if key.Name() == "" {
			b.state.err = safeerror.NewFault(fault.Invalid, "telemetry readiness requires a service key")
		} else if b.state.telemetryReadiness.Name() != "" {
			b.state.err = safeerror.NewFault(fault.Duplicate, "telemetry readiness is already declared")
		} else {
			b.state.telemetryReadiness = key
		}
	})
	return b
}

// Telemetry resolves the configured engine, or returns a missing fault when disabled.
func (s Services) Telemetry() (*telemetry.Engine, error) { return Resolve(s, TelemetryKey) }

func validateTelemetrySettings(s TelemetrySettings, observation ObservabilitySettings) error {
	if !observation.Enabled {
		return safeerror.NewFault(fault.Invalid, "telemetry requires configured observability")
	}
	if err := s.Collector.Validate(); err != nil {
		return err
	}
	if err := s.Config.Validate(); err != nil {
		return err
	}
	for i, kernel := range s.Kernels {
		switch kernel {
		case foundation.HTTP, foundation.Worker, foundation.Scheduler, foundation.CLI, foundation.WebSocket:
		default:
			return safeerror.NewFault(fault.Invalid, "telemetry kernels must name known kernels")
		}
		if slices.Contains(s.Kernels[:i], kernel) {
			return safeerror.NewFault(fault.Duplicate, "telemetry kernel is listed more than once")
		}
	}
	return nil
}

func registerTelemetry(ctx context.Context, builder *foundation.Builder, settings TelemetrySettings, collector *telemetry.Collector, readiness foundation.Key[*diagnostics.Runtime]) error {
	if !settings.Enabled {
		return nil
	}
	requires := []foundation.ProviderID{Provider, infrastructure.DatabaseProvider(settings.Database), MetricsProvider}
	construct := func(r foundation.Resolver) (*telemetry.Engine, error) {
		services, err := FromResolver(r)
		if err != nil {
			return nil, err
		}
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(settings.Database))
		if err != nil {
			return nil, err
		}
		var readinessRuntime *diagnostics.Runtime
		if readiness.Name() != "" {
			readinessRuntime, err = foundation.Resolve(r, readiness)
			if err != nil {
				return nil, err
			}
		}
		return telemetry.New(collector, telemetry.Dependencies{DB: db, Schema: settings.Schema, Recorder: services.recorder, Readiness: readinessRuntime, Clock: services.clock, Logger: services.Logger}, settings.Config)
	}
	builder.Register(telemetry.KernelModule(TelemetryProvider, TelemetryKey, settings.Kernels, requires, construct))
	if readiness.Name() == "" {
		return nil
	}
	// Inspect invokes pure registrations only. Service names need not equal
	// their provider IDs, so use registered ownership to order boot and cleanup.
	inspection, err := builder.Inspect(ctx)
	if err != nil {
		return err
	}
	for _, contribution := range inspection.Contributions {
		if contribution.Name != readiness.Name() {
			continue
		}
		if contribution.Type != reflect.TypeFor[*diagnostics.Runtime]().String() {
			return safeerror.NewFault(fault.Invalid, "telemetry readiness key has a different service type")
		}
		if !slices.Contains(requires, contribution.Owner) {
			requires = append(requires, contribution.Owner)
		}
		builder.Replace(telemetry.KernelModule(TelemetryProvider, TelemetryKey, settings.Kernels, requires, construct))
		return nil
	}
	return safeerror.NewFault(fault.Missing, "telemetry readiness service is not registered")
}
