package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// BrowserTokenRoutes serves the same token guard to a browser SPA: the access
// token stays a bearer credential in JSON, while the refresh token travels only
// in the HttpOnly refresh cookie. name prefixes the route IDs and paths so two
// guards, each with its own cookie, can share one origin.
func BrowserTokenRoutes(tokens *UserTokens, clock clock.Clock, verify VerifiedLogin, cookie foundryhttp.RefreshCookie, name string) (*foundryhttp.Router, error) {
	guard := tokens.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		return nil, err
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
	if err != nil {
		return nil, err
	}
	route := func(id string, access foundryhttp.Access) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID(name + "." + id), Method: foundryhttp.POST, Access: access}, foundryhttp.StaticPath("/"+name+"/"+id))
	}
	response := foundryhttp.TokenCookieResponse[models.User, model.ID[models.User]](cookie, 200, clock)
	login := foundryhttp.DefineEndpoint(route("login", foundryhttp.Public), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response).Handle(func(ctx context.Context, _ ProfileInput) (IssuedUserToken, error) {
		proof, err := verify(ctx)
		if err != nil {
			return IssuedUserToken{}, err
		}
		return StartVerifiedToken(ctx, tokens, proof, "Browser", true)
	})
	// The handler is the same as for the JSON refresh body.
	refresh := foundryhttp.DefineEndpoint(route("refresh", foundryhttp.Public), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), response).Handle(func(ctx context.Context, input RefreshInput) (IssuedUserToken, error) {
		return RefreshFromBody(ctx, tokens, input.Body)
	})
	logout := foundryhttp.RequireAuthentication(foundryhttp.DefineEndpoint(route("logout", foundryhttp.Guarded), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
		foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204))), transport, guard).Handle(func(ctx context.Context, _ models.User, _ ProfileInput) (foundryhttp.NoContent, error) {
		_, err := tokens.RevokeCurrent(ctx)
		return foundryhttp.NoContent{}, err
	})
	return foundryhttp.NewRouter(login, refresh, logout)
}
