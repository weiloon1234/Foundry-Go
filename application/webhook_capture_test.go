package application_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/webhook/capture"
)

type captureOperator struct{ ID int64 }

func (o captureOperator) FoundryIdentity() (model.Identity, error) {
	return captureOperatorReference(o.ID).Identity()
}

func captureOperatorReference(id int64) model.Reference[captureOperator, int64] {
	return model.NewReference[captureOperator]("capture_operators", id, codec.Signed[int64]())
}

func capturePrefix(t *testing.T, path string, options ...http.CaptureOption) http.CaptureDeclaration {
	t.Helper()
	prefix, err := http.ParsePathPrefix(path)
	if err != nil {
		t.Fatal(err)
	}
	return http.CapturePrefix(prefix, options...)
}

// guardedRoutes declares one authenticated route at path, built purely from
// declarations so Build performs no I/O.
func guardedRoutes(path string) application.Routes {
	return func(application.Services) ([]http.RouteRegistration, error) {
		provider := auth.DefineProvider("capture.operators", captureOperatorReference(0), func(_ context.Context, id int64) (value.Optional[captureOperator], error) {
			return value.Set(captureOperator{ID: id}), nil
		}, func(context.Context, captureOperator) (bool, error) { return true, nil })
		strategy := auth.DefineStrategy("capture.bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[captureOperator, int64]], error) {
			return value.Optional[auth.Proof[captureOperator, int64]]{}, nil
		})
		guard := auth.DefineGuard("capture.operators", provider, strategy)
		registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
		if err != nil {
			return nil, err
		}
		transport, err := http.NewAuthentication(registry, http.BearerCredential("capture.bearer"))
		if err != nil {
			return nil, err
		}
		binding, err := http.BindGuard(transport, guard)
		if err != nil {
			return nil, err
		}
		endpoint := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "capture.guarded", Method: http.POST, Access: http.Guarded}, http.StaticPath(path)), http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
		return []http.RouteRegistration{http.Authenticated(endpoint, binding).Handle(func(context.Context, captureOperator, http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
			return http.NoContent{}, nil
		})}, nil
	}
}

// captureSettings configures a database Build never connects to.
func captureSettings() application.Settings {
	s := settings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Database, connection.Primary.User, connection.Primary.Port = "127.0.0.1", "fixture", "fixture", 1
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	return s
}

func captureOrigins(app *application.App) []string {
	var origins []string
	for _, target := range app.Migrations() {
		for _, definition := range target.Definitions {
			if definition.Key.Origin == capture.MigrationOrigin {
				origins = append(origins, string(target.Connection)+"/"+target.Schema)
			}
		}
	}
	return origins
}

func TestWebhookCaptureSettingsDefaults(t *testing.T) {
	defaults := application.DefaultSettings().Features.WebhookCapture
	if !defaults.Enabled || defaults.Config != capture.DefaultConfig() || defaults.Database != "" || defaults.Schema != "" {
		t.Fatal("webhook capture defaults changed")
	}
	if _, err := application.WebhookCaptureCommand(); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookCaptureBuildFailures(t *testing.T) {
	disabled := settings()
	disabled.HTTP.Enabled = false
	if _, err := application.New(disabled, quiet()).CaptureWebhooks(capturePrefix(t, "/wh")).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("capture without HTTP passed Build", err)
	}
	for name, build := range map[string]func(*application.Builder) *application.Builder{
		"root": func(b *application.Builder) *application.Builder { return b.CaptureWebhooks(capturePrefix(t, "/")) },
		"duplicate": func(b *application.Builder) *application.Builder {
			return b.CaptureWebhooks(capturePrefix(t, "/wh")).CaptureWebhooks(capturePrefix(t, "/wh", http.FullCaptureBody()))
		},
		"zero declaration": func(b *application.Builder) *application.Builder { return b.CaptureWebhooks(http.CaptureDeclaration{}) },
		"too many": func(b *application.Builder) *application.Builder {
			return b.CaptureWebhooks(make([]http.CaptureDeclaration, 33)...)
		},
		"invalid limits": func(b *application.Builder) *application.Builder {
			return b.CaptureWebhooks(capturePrefix(t, "/wh"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := captureSettings()
			if name == "invalid limits" {
				s.Features.WebhookCapture.Config.MaxRows = 1
			}
			if _, err := build(application.New(s, quiet()).HTTP(plainRoutes)).Build(t.Context()); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid capture declaration passed Build", err)
			}
		})
	}
}

func TestWebhookCaptureRejectsGuardedRoutesInConfiguredAssembly(t *testing.T) {
	root, err := http.ParsePathPrefix("/account")
	if err != nil {
		t.Fatal(err)
	}
	fallbacks := func(application.Services) ([]http.Fallback, error) {
		return []http.Fallback{http.DefineFallback("account", root, stdhttp.NotFoundHandler(), http.DefaultFallbackConfig())}, nil
	}
	assets := foundation.NewKey[*http.Assets]("capture.portal")
	portal := http.AssetsModule("capture.portal", assets, http.DefaultAssetsConfig(http.FilesystemAssets(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}})))
	build := func(path string) error {
		app, err := application.New(captureSettings(), quiet()).Register(portal).SPA("portal", assets, http.DefaultSPAConfig()).
			HTTP(plainRoutes, guardedRoutes(path)).Fallback(fallbacks).CaptureWebhooks(capturePrefix(t, "/wh")).Build(t.Context())
		if err == nil {
			stop(t, app)
		}
		return err
	}
	if err := build("/wh/session"); !errors.Is(err, fault.Invalid) {
		t.Fatal("authenticated route beneath a capture prefix passed the configured assembly", err)
	}
	if err := build("/admin/session"); err != nil {
		t.Fatal("unrelated authenticated route was rejected", err)
	}
}

func TestWebhookCaptureWiring(t *testing.T) {
	undeclared, err := application.New(settings(), quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, undeclared) })
	if _, err := undeclared.Resources().WebhookCaptures(); !errors.Is(err, fault.Missing) || len(captureOrigins(undeclared)) != 0 {
		t.Fatal("undeclared capture registered a store or migration", err)
	}
	for _, enabled := range []bool{true, false} {
		s := captureSettings()
		s.Features.WebhookCapture.Enabled = enabled
		app, err := application.New(s, quiet()).HTTP(plainRoutes).CaptureWebhooks(capturePrefix(t, "/wh"), capturePrefix(t, "/wh/debug", http.FullCaptureBody())).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, app) })
		if _, err := app.Resources().WebhookCaptures(); err != nil {
			t.Fatal("declared capture store did not resolve", enabled, err)
		}
		if origins := captureOrigins(app); len(origins) != 1 || origins[0] != "default/public" {
			t.Fatal("capture migration target changed", origins)
		}
	}
}
