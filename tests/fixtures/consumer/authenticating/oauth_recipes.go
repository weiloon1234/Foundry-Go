package authenticating

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/oauth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func socialProviders(googleID string, googleSecret secret.String, githubID string, githubSecret secret.String) (oauth.Provider, oauth.Provider) {
	// doc-example:begin social-providers
	google := oauth.Google(oauth.Credentials{
		ClientID:     googleID,
		ClientSecret: googleSecret, // Loaded through the application's secret configuration.
		RedirectURL:  "https://app.example/auth/google/callback",
	})
	github := oauth.GitHub(oauth.Credentials{
		ClientID:     githubID,
		ClientSecret: githubSecret,
		RedirectURL:  "https://app.example/auth/github/callback",
	})
	// doc-example:end social-providers
	return google, github
}

func socialClient(ctx context.Context, google oauth.Provider) (*oauth.Client, *httpclient.Client, error) {
	// doc-example:begin social-client
	policy, err := google.Destinations()
	if err != nil {
		return nil, nil, err
	}
	config := httpclient.DefaultConfig("oauth.google")
	config.Destination = policy
	outbound, err := httpclient.New(config, nil)
	if err != nil {
		return nil, nil, err
	}
	client, err := oauth.NewClient(google, outbound, clock.System{})
	if err != nil {
		return nil, nil, errors.Join(err, outbound.Close(ctx))
	}
	// The application owns outbound and closes it during shutdown.
	// doc-example:end social-client
	return client, outbound, nil
}

func socialLoginRoutes(services application.Services, client *oauth.Client, web *http.BrowserSessions[models.User, model.ID[models.User]], handoffs SocialHandoffs) (*http.Router, error) {
	// doc-example:begin social-flow-routes
	encrypter, err := services.CookieEncrypter()
	if err != nil {
		return nil, err
	}
	flow, err := oauth.NewFlow(client, encrypter, "__Host-foundry_oauth_google")
	if err != nil {
		return nil, err
	}
	cookie := http.DefineCookie("__Host-social-handoff", http.SecretCookie(), http.DefaultCookieOptions())

	startRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google"))
	start := http.HandleRaw(startRoute, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
		location, err := flow.Start(r.Context(), w)
		if err != nil {
			http.WriteError(w, r, err)
			return
		}
		stdhttp.Redirect(w, r, location, stdhttp.StatusFound)
	})
	callbackRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google.callback", Method: http.GET, Access: http.Public}, http.StaticPath("/auth/google/callback"))
	callback := http.HandleRaw(callbackRoute, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
		profile, err := flow.Finish(w, r)
		if err != nil {
			http.WriteError(w, r, err)
			return
		}
		handoff, err := handoffs.Stage(r.Context(), profile)
		if err != nil {
			http.WriteError(w, r, err)
			return
		}
		if err := cookie.Set(r.Context(), w, handoff); err != nil {
			http.WriteError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Referrer-Policy", "no-referrer")
		io.WriteString(w, `<form method="post" action="/auth/google/complete"><button>Continue signing in</button></form>`)
	})
	capture := http.DefineMiddleware("auth.google.handoff", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			supplied, err := cookie.Read(r)
			if err != nil {
				http.WriteError(w, r, err)
				return
			}
			handoff, present := supplied.Get()
			if !present {
				http.WriteError(w, r, auth.Unauthenticated)
				return
			}
			if err := cookie.Clear(w); err != nil {
				http.WriteError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), socialHandoffContextKey{}, handoff)
			next.ServeHTTP(w, r.WithContext(ctx))
		}), nil
	})
	completeRoute := http.DefineRoute(http.RouteSpec{ID: "auth.google.complete", Method: http.POST, Access: http.Public}, http.StaticPath("/auth/google/complete"))
	endpoint := http.DefineEndpoint(completeRoute, http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
	complete := http.ConfigureEndpoint(endpoint, http.Use(web.Middleware(), capture)).Handle(func(ctx context.Context, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
		handoff, ok := ctx.Value(socialHandoffContextKey{}).(secret.String)
		if !ok {
			return http.NoContent{}, auth.Unauthenticated
		}
		proof, err := handoffs.Consume(ctx, handoff)
		if err != nil {
			return http.NoContent{}, err
		}
		_, err = web.Login(ctx, proof, session.IssueOptions{})
		return http.NoContent{}, err
	})
	router, err := http.NewRouter(start, callback, complete)
	// doc-example:end social-flow-routes
	return router, err
}

// doc-example:begin social-handoff-state
// SocialHandoffs is application-owned, bounded, expiring, single-use login state.
// Stage returns an unguessable token and stores only the verified identity needed
// for login. Consume atomically removes it, resolves the current local account
// and returns its proof with the application's required assurance.
type SocialHandoffs interface {
	Stage(context.Context, oauth.Profile) (secret.String, error)
	Consume(context.Context, secret.String) (auth.Proof[models.User, model.ID[models.User]], error)
}

type socialHandoffContextKey struct{}

// doc-example:end social-handoff-state
