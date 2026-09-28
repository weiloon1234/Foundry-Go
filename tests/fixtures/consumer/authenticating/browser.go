package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Browser = foundryhttp.BrowserSessions[models.User, model.ID[models.User]]
type VerifiedLogin func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error)

// BrowserLogin keeps credential publication inside Foundry. The proof is supplied
// by a trusted authentication flow; this fixture does not implement passwords.
func BrowserLogin(ctx context.Context, web *Browser, proof auth.Proof[models.User, model.ID[models.User]]) (UserSession, error) {
	return web.Login(ctx, proof, session.IssueOptions{})
}
func BrowserRotate(ctx context.Context, web *Browser) (UserSession, error) { return web.Rotate(ctx) }
func BrowserLogout(ctx context.Context, web *Browser) error                { return web.Logout(ctx) }

// BrowserRoutes uses ordinary DTO-preserving endpoints. Public login/logout
// install browser middleware; guarded routes receive it through Authentication.
func BrowserRoutes(web *Browser, verify VerifiedLogin) (*foundryhttp.Router, error) {
	endpoint := func(id foundryhttp.RouteID, method foundryhttp.Method, path string, access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent] {
		return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: access}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	}
	login := endpoint("browser.login", foundryhttp.POST, "/login", foundryhttp.Public).WithMiddleware(web.Middleware()).Handle(func(ctx context.Context, _ ProfileInput) (foundryhttp.NoContent, error) {
		proof, err := verify(ctx)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		_, err = BrowserLogin(ctx, web, proof)
		return foundryhttp.NoContent{}, err
	})
	profile := foundryhttp.RequireAuthentication(endpoint("browser.profile", foundryhttp.GET, "/profile", foundryhttp.Guarded), web.Authentication(), web.Guard()).Handle(func(ctx context.Context, user models.User, _ ProfileInput) (foundryhttp.NoContent, error) {
		// Domain code receives the concrete model, and can use its typed getters.
		_, err := web.Guard().Require(ctx)
		return foundryhttp.NoContent{}, err
	})
	rotate := foundryhttp.RequireAuthentication(endpoint("browser.rotate", foundryhttp.POST, "/rotate", foundryhttp.Guarded), web.Authentication(), web.Guard()).Handle(func(ctx context.Context, _ models.User, _ ProfileInput) (foundryhttp.NoContent, error) {
		_, err := BrowserRotate(ctx, web)
		return foundryhttp.NoContent{}, err
	})
	logout := endpoint("browser.logout", foundryhttp.POST, "/logout", foundryhttp.Public).WithMiddleware(web.Middleware()).Handle(func(ctx context.Context, _ ProfileInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, BrowserLogout(ctx, web)
	})
	return foundryhttp.NewRouter(login, profile, rotate, logout)
}
