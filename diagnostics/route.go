package diagnostics

import (
	"encoding/json"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type Endpoint string

const (
	Liveness  Endpoint = "liveness"
	Readiness Endpoint = "readiness"
	Status    Endpoint = "status"
	Metrics   Endpoint = "metrics"
	// Profile serves runtime/pprof profiles. It additionally requires
	// Config.Profiling; see ProfileQuery for the accepted parameters.
	Profile Endpoint = "profile"
)

// Route requires a concrete authenticated GET route. Configure its permissions
// and scopes with normal auth APIs before binding. No implicit public route,
// credential bypass, configuration dump or listener is created. The profiler is
// available only through an explicitly bound Profile route with Profiling set.
func Route[M any](runtime *Runtime, route foundryhttp.AuthenticatedRoute[foundryhttp.NoPath, M], endpoint Endpoint) foundryhttp.RouteRegistration {
	if runtime == nil || runtime.slots == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "diagnostics requires an initialized runtime"))
	}
	if endpoint != Liveness && endpoint != Readiness && endpoint != Status && endpoint != Metrics && endpoint != Profile {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "unknown diagnostics endpoint"))
	}
	if endpoint == Profile && !runtime.profiling {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "diagnostics profiling is not enabled"))
	}
	description, err := route.Description()
	if err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if description.Method != foundryhttp.GET {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "diagnostics requires a GET route"))
	}
	return route.HandleRaw(func(writer stdhttp.ResponseWriter, request *stdhttp.Request, _ M, _ foundryhttp.NoPath) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		select {
		case runtime.slots <- struct{}{}:
			defer func() { <-runtime.slots }()
		default:
			_ = foundryhttp.WriteError(writer, request, foundryhttp.Unavailable)
			return
		}
		if endpoint == Profile {
			runtime.serveProfile(writer, request)
			return
		}
		if endpoint == Metrics {
			writer.Header().Set("Content-Type", observability.PrometheusContentType)
			writer.WriteHeader(stdhttp.StatusOK)
			if request.Method != stdhttp.MethodHead {
				_, recorder, _ := runtime.source()
				_ = recorder.WritePrometheus(writer)
			}
			return
		}
		status := stdhttp.StatusOK
		var payload any
		switch endpoint {
		case Liveness:
			report := runtime.Liveness()
			payload = report
			if !report.Live {
				status = stdhttp.StatusServiceUnavailable
			}
		case Readiness:
			report, _ := runtime.Readiness(request.Context())
			payload = report
			if !report.Ready {
				status = stdhttp.StatusServiceUnavailable
			}
		case Status:
			payload = runtime.Snapshot()
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if request.Method != stdhttp.MethodHead {
			_ = json.NewEncoder(writer).Encode(payload)
		}
	})
}
