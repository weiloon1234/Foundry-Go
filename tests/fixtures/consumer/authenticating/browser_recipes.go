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

// doc-example:begin browser-session-transport
func browserSessionTransport(sessions *session.Sessions[models.User, model.ID[models.User]]) (*http.BrowserSessions[models.User, model.ID[models.User]], error) {
	registry, err := auth.NewRegistry(auth.DefaultConfig(), sessions.Guard().Registration())
	if err != nil {
		return nil, err
	}
	return http.NewBrowserSessions(registry, sessions, http.DefaultBrowserSessionConfig())
}

// doc-example:end browser-session-transport

// doc-example:begin remember-browser-login
func rememberBrowserLogin(ctx context.Context, web *http.BrowserSessions[models.User, model.ID[models.User]], verifiedProof auth.Proof[models.User, model.ID[models.User]]) (session.Info[models.User, model.ID[models.User]], error) {
	info, err := web.Login(ctx, verifiedProof, session.IssueOptions{Remember: true})
	if err != nil {
		return session.Info[models.User, model.ID[models.User]]{}, err
	}
	// Build the response DTO from allowed presentation values.
	return info, nil
}

// doc-example:end remember-browser-login

// doc-example:begin rotate-and-logout-browser
func rotateBrowser(ctx context.Context, web *http.BrowserSessions[models.User, model.ID[models.User]]) (session.Info[models.User, model.ID[models.User]], error) {
	return web.Rotate(ctx)
}

func logoutBrowser(ctx context.Context, web *http.BrowserSessions[models.User, model.ID[models.User]]) error {
	return web.Logout(ctx)
}

// doc-example:end rotate-and-logout-browser

// doc-example:begin confirm-password-route
func confirmPasswordRoute(endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.NoContent], browser *http.BrowserSessions[models.User, model.ID[models.User]]) http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent] {
	return http.RequireAuthentication(endpoint, browser.Authentication(), browser.Guard(),
		http.ActorMiddleware[models.User](browser.RequirePasswordConfirmation(15*time.Minute)))
}

// doc-example:end confirm-password-route

// doc-example:begin typed-browser-login
func browserLoginRoute(web *http.BrowserSessions[models.User, model.ID[models.User]], verify func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error)) http.RouteRegistration {
	route := http.DefineRoute(http.RouteSpec{ID: "browser.login", Method: http.POST, Access: http.Public}, http.StaticPath("/login"))
	endpoint := http.DefineEndpoint(route, http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
	return http.ConfigureEndpoint(endpoint, http.Use(web.Middleware())).Handle(func(ctx context.Context, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
		proof, err := verify(ctx)
		if err != nil {
			return http.NoContent{}, err
		}
		_, err = web.Login(ctx, proof, session.IssueOptions{Remember: true})
		return http.NoContent{}, err
	})
}

// doc-example:end typed-browser-login
