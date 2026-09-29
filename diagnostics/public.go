package diagnostics

import (
	"context"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

// Public probe bodies deliberately omit lifecycle, maintenance and dependency
// detail; the authenticated Route serves those reports.
var (
	liveBody      = []byte(`{"status":"up"}` + "\n")
	downBody      = []byte(`{"status":"down"}` + "\n")
	readyBody     = []byte(`{"status":"ready"}` + "\n")
	unreadyBody   = []byte(`{"status":"unavailable"}` + "\n")
	probeEndpoint = map[Endpoint]bool{Liveness: true, Readiness: true}
)

// DefaultReadinessCache is how long a public readiness route reuses one
// dependency check.
const DefaultReadinessCache = time.Second

// PublicProbeOption adjusts a public probe route.
type PublicProbeOption func(*publicProbeOptions) error
type publicProbeOptions struct{ cache time.Duration }

// ReadinessCache sets how long public readiness reuses one dependency check,
// from 100ms to one minute.
func ReadinessCache(ttl time.Duration) PublicProbeOption {
	return func(o *publicProbeOptions) error {
		if ttl < 100*time.Millisecond || ttl > time.Minute {
			return fault.New(fault.Invalid, "public readiness cache must be from 100ms to one minute")
		}
		o.cache = ttl
		return nil
	}
}

// PublicProbe registers an unauthenticated GET route for load balancers and
// orchestrators. Liveness reports only whether the application is still live and
// performs no dependency checks. Readiness reads lifecycle and maintenance state
// live and answers dependencies from the result of one check reused for
// ReadinessCache (DefaultReadinessCache): a single request refreshes it while
// others receive the previous answer, so a flood of unauthenticated requests
// cannot multiply dependency I/O. Responses are 200 or 503 without detail.
// Public probes never use the diagnostics operation slots.
func PublicProbe(runtime *Runtime, id foundryhttp.RouteID, path string, endpoint Endpoint, options ...PublicProbeOption) foundryhttp.RouteRegistration {
	if runtime == nil || runtime.slots == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "public probe requires an initialized runtime"))
	}
	if !probeEndpoint[endpoint] {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "public probes serve only liveness or readiness"))
	}
	settings := publicProbeOptions{cache: DefaultReadinessCache}
	for _, option := range options {
		if option == nil {
			return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "nil public probe option"))
		}
		if err := option(&settings); err != nil {
			return foundryhttp.InvalidRouteRegistration(err)
		}
	}
	cache := &readinessCache{runtime: runtime, ttl: settings.cache}
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	return route.HandleRaw(func(writer stdhttp.ResponseWriter, request *stdhttp.Request, _ foundryhttp.NoPath) {
		header := writer.Header()
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		status, body := stdhttp.StatusOK, liveBody
		if endpoint == Liveness {
			if !runtime.Liveness().Live {
				status, body = stdhttp.StatusServiceUnavailable, downBody
			}
		} else {
			body = readyBody
			if !cache.ready(request.Context()) {
				status, body = stdhttp.StatusServiceUnavailable, unreadyBody
			}
		}
		header.Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if request.Method != stdhttp.MethodHead {
			_, _ = writer.Write(body)
		}
	})
}

// readinessCache shares one dependency check among public requests. Only
// the request that finds the result expired runs the check; while it runs,
// others reuse the previous answer (or wait for the first one).
type readinessCache struct {
	runtime *Runtime
	ttl     time.Duration
	mu      sync.Mutex
	value   bool
	checked time.Time
	flight  chan struct{}
}

func (c *readinessCache) ready(ctx context.Context) bool {
	// A stop delay, shutdown or pause is reported at once, without I/O.
	state, _, gate := c.runtime.source()
	if state != foundation.Running || gate.Mode() != maintenance.Serving {
		return false
	}
	c.mu.Lock()
	if !c.checked.IsZero() && time.Since(c.checked) < c.ttl {
		value := c.value
		c.mu.Unlock()
		return value
	}
	if flight := c.flight; flight != nil {
		value, known := c.value, !c.checked.IsZero()
		c.mu.Unlock()
		if known {
			return value
		}
		select {
		case <-flight:
		case <-ctx.Done():
			return false
		}
		c.mu.Lock()
		value, known = c.value, !c.checked.IsZero()
		c.mu.Unlock()
		return known && value
	}
	flight := make(chan struct{})
	c.flight = flight
	c.mu.Unlock()
	report, err := c.runtime.Readiness(ctx)
	value := err == nil && report.Ready
	c.mu.Lock()
	c.flight = nil
	// A caller that went away does not poison the shared answer.
	if ctx.Err() == nil {
		c.value, c.checked = value, time.Now()
	}
	c.mu.Unlock()
	close(flight)
	return value
}
