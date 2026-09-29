package application_test

import (
	"context"
	"io"
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// A configured sticky read window gives every HTTP request its own
// read-your-writes scope; without one no scope (and no middleware) is added.
func TestConfiguredStickyReadsScopeEveryRequest(t *testing.T) {
	primary := infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	for name, window := range map[string]time.Duration{"configured": time.Second, "absent": 0} {
		t.Run(name, func(t *testing.T) {
			s := settings()
			connection := infrastructure.DefaultConnectionSettings()
			connection.Primary, connection.Read, connection.ReadEnabled = primary, primary, true
			connection.StickyReadWindow = window
			s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
			scoped := func(application.Services) ([]http.RouteRegistration, error) {
				return []http.RouteRegistration{http.DefineRoute(http.RouteSpec{ID: "fixture.sticky", Method: http.GET, Access: http.Public}, http.StaticPath("/sticky")).HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
					// StickyReads returns a scoped context unchanged.
					if database.StickyReads(r.Context()) == r.Context() {
						_, _ = io.WriteString(w, "scoped")
						return
					}
					_, _ = io.WriteString(w, "unscoped")
				})}, nil
			}
			app, err := application.New(s, quiet()).HTTP(scoped).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- app.Run(ctx, foundation.HTTP) }()
			ready, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
			address, err := app.HTTPReady(ready)
			readyCancel()
			if err != nil {
				t.Fatal(err)
			}
			response, err := (&stdhttp.Client{Timeout: 3 * time.Second}).Get("http://" + address + "/sticky")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			want := map[string]string{"configured": "scoped", "absent": "unscoped"}[name]
			if string(body) != want {
				t.Fatal("sticky read scope", string(body), "want", want)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
