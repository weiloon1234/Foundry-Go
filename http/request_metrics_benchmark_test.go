package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/observability"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkRequestMetricsObserve(b *testing.B) {
	metrics, err := NewRequestMetrics(1024)
	if err != nil {
		b.Fatal(err)
	}
	event := RequestEvent{Route: "accounts.show", Method: GET, Result: observability.Result{Status: 200}, Duration: 20 * time.Millisecond}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		metrics.ObserveRequest(ctx, event)
	}
}
func BenchmarkRequestMetricsObserveParallel(b *testing.B) {
	metrics, err := NewRequestMetrics(1024)
	if err != nil {
		b.Fatal(err)
	}
	event := RequestEvent{Route: "accounts.show", Method: GET, Result: observability.Result{Status: 200}, Duration: 20 * time.Millisecond}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.ObserveRequest(ctx, event)
		}
	})
}
func BenchmarkRequestCompletionMetrics(b *testing.B) {
	route := DefineRoute(RouteSpec{ID: "benchmark.ok", Method: GET, Access: Public}, StaticPath("/ok"))
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }))
	if err != nil {
		b.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			observers := []RequestObserver{}
			if enabled {
				metrics, err := NewRequestMetrics(1024)
				if err != nil {
					b.Fatal(err)
				}
				observers = append(observers, metrics)
			}
			handler := newHandlerLifetime().wrap(router, slog.New(slog.NewJSONHandler(io.Discard, nil)), DefaultServerConfig(), observers...)
			request := httptest.NewRequest("GET", "/ok", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 204 {
					b.Fatal(response.Code)
				}
			}
		})
	}
}
