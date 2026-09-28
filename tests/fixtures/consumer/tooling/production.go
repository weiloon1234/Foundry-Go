package tooling

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/observability"
)

const ReadinessProvider foundation.ProviderID = "production.readiness"

var ReadinessKey = foundation.NewKey[*health.Registry]("production.readiness")
var DiagnosticsKey = foundation.NewKey[*diagnostics.Runtime]("production.diagnostics")

func ReadinessModule(requires []foundation.ProviderID, probes func(foundation.Resolver) ([]health.Probe, error)) foundation.Module {
	return health.Module(ReadinessProvider, ReadinessKey, health.DefaultConfig(), requires, probes)
}
func DiagnosticsModule() foundation.Module {
	return diagnostics.Module("production.diagnostics", DiagnosticsKey, ReadinessKey, diagnostics.DefaultConfig(), []foundation.ProviderID{ReadinessProvider})
}

var LiveRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "operations.live", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/operations/live"))
var ReadyRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "operations.ready", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/operations/ready"))
var StatusRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "operations.status", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/operations/status"))
var MetricsRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "operations.metrics", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/operations/metrics"))

// OperationalRoutes requires the application's concrete operator guard and
// permission. Maintenance paths are derived from those same route descriptors.
func OperationalRoutes(runtime *diagnostics.Runtime, authentication *foundryhttp.Authentication, guard auth.Guard[models.User], permission auth.Permission[models.User]) ([]foundryhttp.RouteRegistration, []string, error) {
	var registrations []foundryhttp.RouteRegistration
	var paths []string
	for _, item := range []struct {
		route    foundryhttp.Route[foundryhttp.NoPath]
		endpoint diagnostics.Endpoint
	}{
		{LiveRoute, diagnostics.Liveness}, {ReadyRoute, diagnostics.Readiness}, {StatusRoute, diagnostics.Status}, {MetricsRoute, diagnostics.Metrics},
	} {
		protected := foundryhttp.RequireRouteAuthentication(item.route, authentication, guard).WithPermissions(permission)
		path, err := protected.URL(foundryhttp.NoPath{})
		if err != nil {
			return nil, nil, err
		}
		registrations = append(registrations, diagnostics.Route(runtime, protected, item.endpoint))
		paths = append(paths, path)
	}
	return registrations, paths, nil
}

func ObservedTask(ctx context.Context, recorder *observability.Recorder, name observability.Name, run func(context.Context) error) error {
	return observability.Observe(observability.WithContext(ctx, recorder), observability.Operation{Kind: observability.Resource, Name: name}, run)
}

// TracedPending is enabled only after all worker/outbox readers support format 2.
func TracedPending[P any](ctx context.Context, definition jobs.Definition[P], input P) (jobs.Pending[P], error) {
	return definition.Capture(ctx, input, jobs.Options[P]{PropagateTrace: true})
}
