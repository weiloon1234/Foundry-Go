package configuredprofile

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/http"
	"io"
	stdhttp "net/http"
)

var Greetings = cache.Define("profile.greetings", cache.StringKeys[string](), cache.JSON[string]())

// Routes captures a concrete typed cache. No settings decoding or service lookup
// occurs while serving a request.
func Routes(services application.Services) ([]http.RouteRegistration, error) {
	store, err := services.Cache()
	if err != nil {
		return nil, err
	}
	values, err := Greetings.Bind(store)
	if err != nil {
		return nil, err
	}
	route := http.DefineRoute(http.RouteSpec{ID: "profile.greeting", Method: http.GET, Access: http.Public}, http.StaticPath("/profile"))
	return []http.RouteRegistration{route.HandleRaw(func(w stdhttp.ResponseWriter, request *stdhttp.Request, _ http.NoPath) {
		value, err := values.Remember(request.Context(), "message", cache.Forever(), func(context.Context) (string, error) { return "profile", nil })
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, value)
	})}, nil
}

func NamedCache(resources application.Services) (*cache.Store, error) {
	return resources.Caches.Store(Reports)
}
