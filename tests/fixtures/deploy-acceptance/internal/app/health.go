package app

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http"
)

//foundry:dto
type HealthLive struct {
	Status string `json:"status"`
}

//foundry:dto
type HealthReady struct {
	Ready bool `json:"ready"`
}

type healthInput = http.Input[http.NoPath, http.NoQuery, http.NoBody]

func healthRoutes(services application.Services) ([]http.RouteRegistration, error) {
	// Reuse the built-in probe runtime without registering an opaque job provider.
	runtime, err := application.Resolve(services, application.ProbeKey)
	if err != nil {
		return nil, err
	}
	noCache := http.DefineMiddleware("health.no-cache", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		}), nil
	})
	live := http.ConfigureEndpoint(http.DefineEndpoint(
		http.DefineRoute(http.RouteSpec{ID: "health.live", Method: http.GET, Access: http.Public}, http.StaticPath("/health/live")),
		http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, HealthLiveJSON()),
	), http.Use(noCache))
	ready := http.ConfigureEndpoint(http.DefineEndpoint(
		http.DefineRoute(http.RouteSpec{ID: "health.ready", Method: http.GET, Access: http.Public}, http.StaticPath("/health/ready")),
		http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, HealthReadyJSON()),
	), http.Use(noCache))
	return []http.RouteRegistration{
		live.Handle(func(context.Context, healthInput) (HealthLive, error) {
			return HealthLive{Status: "ok"}, nil
		}),
		ready.Handle(func(ctx context.Context, _ healthInput) (HealthReady, error) {
			report, err := runtime.Readiness(ctx)
			if err != nil || !report.Ready {
				return HealthReady{}, http.Unavailable.WithCause(err)
			}
			return HealthReady{Ready: true}, nil
		}),
	}, nil
}
