package authenticating

import (
	"context"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin browser-session-type
type UserBrowserSessions = http.BrowserSessions[models.User, model.ID[models.User]]

// doc-example:end browser-session-type

func browserSessionTransport(sessions *session.Sessions[models.User, model.ID[models.User]]) (*UserBrowserSessions, error) {
	// doc-example:begin browser-session-transport
	registry, err := auth.NewRegistry(auth.DefaultConfig(), sessions.Guard().Registration())
	if err != nil {
		return nil, err
	}
	web, err := http.NewBrowserSessions(registry, sessions, http.DefaultBrowserSessionConfig())
	// doc-example:end browser-session-transport
	return web, err
}

func rememberBrowserLogin(ctx context.Context, web *UserBrowserSessions, verifiedProof auth.Proof[models.User, model.ID[models.User]]) (session.Info[models.User, model.ID[models.User]], error) {
	// doc-example:begin remember-browser-login
	info, err := web.Login(ctx, verifiedProof, session.IssueOptions{Remember: true})
	if err != nil {
		return session.Info[models.User, model.ID[models.User]]{}, err
	}
	// Build the response DTO from allowed presentation values.
	// doc-example:end remember-browser-login
	return info, nil
}

// doc-example:begin rotate-and-logout-browser
func rotateBrowser(ctx context.Context, web *UserBrowserSessions) (session.Info[models.User, model.ID[models.User]], error) {
	return web.Rotate(ctx)
}

func logoutBrowser(ctx context.Context, web *UserBrowserSessions) error {
	return web.Logout(ctx)
}

// doc-example:end rotate-and-logout-browser

func confirmPasswordRoute(endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.NoContent], browser *UserBrowserSessions) http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent] {
	// doc-example:begin confirm-password-route
	route := http.RequireAuthentication(endpoint, browser.Authentication(), browser.Guard(),
		http.ActorMiddleware[models.User](browser.RequirePasswordConfirmation(15*time.Minute)))
	// doc-example:end confirm-password-route
	return route
}

func browserLoginRoute(web *UserBrowserSessions, verify func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error)) http.RouteRegistration {
	// doc-example:begin typed-browser-login
	route := http.DefineRoute(http.RouteSpec{ID: "browser.login", Method: http.POST, Access: http.Public}, http.StaticPath("/login"))
	endpoint := http.DefineEndpoint(route, http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
	registration := http.ConfigureEndpoint(endpoint, http.Use(web.Middleware())).Handle(func(ctx context.Context, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
		proof, err := verify(ctx)
		if err != nil {
			return http.NoContent{}, err
		}
		_, err = web.Login(ctx, proof, session.IssueOptions{Remember: true})
		return http.NoContent{}, err
	})
	// doc-example:end typed-browser-login
	return registration
}
