package app

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http"
)

//foundry:dto http=response
type HealthLive struct {
	Status string `json:"status"`
}

//foundry:dto http=response
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
	bindingProbe, err := featureBindingProbe()
	if err != nil {
		return nil, err
	}
	noCache := http.DefineMiddleware("health.no-cache", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		}), nil
	})

	liveRoute := http.DefineRoute(http.RouteSpec{ID: "health.live", Method: http.GET, Access: http.Public}, http.StaticPath("/health/live"))
	live := http.DefineEndpoint(
		liveRoute,
		http.EmptyQuery(), http.EmptyBody(), HealthLiveResponse(200),
	)
	live = http.ConfigureEndpoint(live, http.Use(noCache))

	readyRoute := http.DefineRoute(http.RouteSpec{ID: "health.ready", Method: http.GET, Access: http.Public}, http.StaticPath("/health/ready"))
	ready := http.DefineEndpoint(
		readyRoute,
		http.EmptyQuery(), http.EmptyBody(), HealthReadyResponse(200),
	)
	ready = http.ConfigureEndpoint(ready, http.Use(noCache))

	return []http.RouteRegistration{
		live.Handle(func(context.Context, healthInput) (HealthLive, error) {
			return HealthLive{Status: "ok"}, nil
		}),
		ready.Handle(func(ctx context.Context, _ healthInput) (HealthReady, error) {
			if err := bindingProbe(ctx); err != nil {
				return HealthReady{}, http.Unavailable.WithCause(err)
			}
			report, err := runtime.Readiness(ctx)
			if err != nil || !report.Ready {
				return HealthReady{}, http.Unavailable.WithCause(err)
			}
			return HealthReady{Ready: true}, nil
		}),
	}, nil
}
