package authenticating

import (
	"context"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin browser-session-type
type UserBrowserSessions = foundryhttp.BrowserSessions[models.User, model.ID[models.User]]

// doc-example:end browser-session-type

func browserSessionTransport(sessions *session.Sessions[models.User, model.ID[models.User]]) (*UserBrowserSessions, error) {
	// doc-example:begin browser-session-transport
	registry, err := auth.NewRegistry(auth.DefaultConfig(), sessions.Guard().Registration())
	if err != nil {
		return nil, err
	}
	return foundryhttp.NewBrowserSessions(registry, sessions, foundryhttp.DefaultBrowserSessionConfig())
	// doc-example:end browser-session-transport
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

func confirmPasswordRoute(endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], browser *UserBrowserSessions) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent] {
	// doc-example:begin confirm-password-route
	route := foundryhttp.RequireAuthentication(endpoint, browser.Authentication(), browser.Guard(),
		foundryhttp.ActorMiddleware[models.User](browser.RequirePasswordConfirmation(15*time.Minute)))
	// doc-example:end confirm-password-route
	return route
}

func browserLoginRoute(web *UserBrowserSessions, verify func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error)) foundryhttp.RouteRegistration {
	// doc-example:begin typed-browser-login
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "browser.login", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/login"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	registration := foundryhttp.ConfigureEndpoint(endpoint, foundryhttp.Use(web.Middleware())).Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		proof, err := verify(ctx)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		_, err = web.Login(ctx, proof, session.IssueOptions{Remember: true})
		return foundryhttp.NoContent{}, err
	})
	// doc-example:end typed-browser-login
	return registration
}
