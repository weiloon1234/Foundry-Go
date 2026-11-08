package application_test

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
)

func TestConfiguredFallbackRoutes(t *testing.T) {
	prefix, err := http.ParsePathPrefix("/account")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	factory := func(services application.Services) ([]http.Fallback, error) {
		calls++
		if _, err := services.Cache(); err != nil {
			return nil, err
		}
		return []http.Fallback{http.DefineFallback("account", prefix, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			info, ok := http.MatchedRoute(r.Context())
			if !ok || info.ID != "account" {
				t.Error("missing fallback identity")
			}
			w.WriteHeader(204)
		}), http.DefaultFallbackConfig())}, nil
	}
	app, err := application.New(settings(), quiet()).HTTP(plainRoutes).Fallback(factory).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	router, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/account/orders/", "/account"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatal("factory called more than once", calls)
	}
}

func TestConfiguredFallbackInvalidDeclarations(t *testing.T) {
	root, err := http.ParsePathPrefix("/")
	if err != nil {
		t.Fatal(err)
	}
	valid := func(application.Services) ([]http.Fallback, error) {
		return []http.Fallback{http.DefineFallback("root", root, stdhttp.NotFoundHandler(), http.DefaultFallbackConfig())}, nil
	}
	for name, configure := range map[string]func(*application.Builder) *application.Builder{
		"nil factory": func(b *application.Builder) *application.Builder { return b.Fallback(nil) },
		"empty declaration": func(b *application.Builder) *application.Builder {
			return b.Fallback(func(application.Services) ([]http.Fallback, error) { return []http.Fallback{{}}, nil })
		},
		"duplicate": func(b *application.Builder) *application.Builder { return b.Fallback(valid, valid) },
		"unknown dependency": func(b *application.Builder) *application.Builder {
			return b.Fallback(func(s application.Services) ([]http.Fallback, error) {
				_, err := application.Resolve(s, foundation.NewKey[*http.Assets]("missing.assets"))
				return nil, err
			})
		},
		"factory error": func(b *application.Builder) *application.Builder {
			return b.Fallback(func(application.Services) ([]http.Fallback, error) { return nil, errors.New("constructor failed") })
		},
		"limit": func(b *application.Builder) *application.Builder {
			return b.Fallback(make([]application.FallbackRoutes, 65)...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			app, err := configure(application.New(settings(), quiet())).Build(t.Context())
			if app != nil || err == nil {
				t.Fatal("accepted invalid declaration", err)
			}
		})
	}
	disabled := settings()
	disabled.HTTP.Enabled = false
	if _, err := application.New(disabled, quiet()).Fallback(valid).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("fallback with disabled HTTP", err)
	}
}
