package configuredprofile

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var selectedStore *cache.Store

func BenchmarkStartup(b *testing.B) {
	settings := Defaults()
	option := quiet()
	b.ReportAllocs()
	for b.Loop() {
		app, err := Build(context.Background(), settings, option)
		if err != nil {
			b.Fatal(err)
		}
		if err = app.Start(context.Background()); err != nil {
			b.Fatal(err)
		}
		if err = app.Shutdown(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkSelection(b *testing.B) {
	app := built(b)
	services := app.Resources()
	retained, err := services.Cache()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("retained", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			selectedStore = retained
		}
	})
	b.Run("default", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			selectedStore, err = services.Cache()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("named", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			selectedStore, err = services.Caches.Store(Primary)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Both cases serve the same route with the same resolved cache and security
// middleware. This isolates router assembly from network/client overhead.
func BenchmarkHTTPHandler(b *testing.B) {
	app := built(b)
	configured, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		b.Fatal(err)
	}
	routes, err := Routes(app.Resources())
	if err != nil {
		b.Fatal(err)
	}
	direct, err := http.NewRouter(routes...)
	if err != nil {
		b.Fatal(err)
	}
	for _, item := range []struct {
		name   string
		router *http.Router
	}{{"direct", direct}, {"configured", configured}} {
		b.Run(item.name, func(b *testing.B) {
			handler, err := http.ApplyMiddleware(item.router, http.SecurityHeaders(http.DefaultSecurityHeadersConfig()))
			if err != nil {
				b.Fatal(err)
			}
			request := httptest.NewRequest(stdhttp.MethodGet, "/profile", nil)
			handler.ServeHTTP(httptest.NewRecorder(), request)
			b.ReportAllocs()
			for b.Loop() {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 200 || response.Body.String() != "profile" {
					b.Fatal("unexpected HTTP response")
				}
			}
		})
	}
}
func BenchmarkHTTPRoundTrip(b *testing.B) {
	app, err := Build(context.Background(), Defaults(), quiet())
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	b.Cleanup(func() {
		cancel()
		stop(b, app)
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			b.Error(err)
		}
	})
	address, err := app.HTTPReady(ctx)
	if err != nil {
		b.Fatal(err)
	}
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	b.Cleanup(transport.CloseIdleConnections)
	client := &stdhttp.Client{Transport: transport, Timeout: 5 * time.Second}
	fetch := func() {
		response, err := client.Get("http://" + address + "/profile")
		if err != nil {
			b.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != 200 || readErr != nil || closeErr != nil {
			b.Fatal(response.StatusCode, readErr, closeErr)
		}
	}
	fetch()
	b.ReportAllocs()
	for b.Loop() {
		fetch()
	}
}
