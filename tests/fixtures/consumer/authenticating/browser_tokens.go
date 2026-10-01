package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RefreshLogoutInput carries the refresh cookie, when the browser presented one.
type RefreshLogoutInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshCookieLogoutRequest]

// BrowserTokenRoutes serves the same token guard to a browser SPA: the access
// token stays a bearer credential in JSON, while the refresh token travels only
// in the HttpOnly refresh cookie. name prefixes the route IDs and paths so two
// guards, each with its own cookie, can share one origin.
func BrowserTokenRoutes(tokens *UserTokens, clock clock.Clock, verify VerifiedLogin, cookie foundryhttp.RefreshCookie, name string) (*foundryhttp.Router, error) {
	route := func(id string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID(name + "." + id), Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+name+"/"+id))
	}
	response := foundryhttp.TokenCookieResponse[models.User, model.ID[models.User]](cookie, 200, clock)
	login := foundryhttp.DefineEndpoint(route("login"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response).Handle(func(ctx context.Context, _ ProfileInput) (IssuedUserToken, error) {
		proof, err := verify(ctx)
		if err != nil {
			return IssuedUserToken{}, err
		}
		return StartVerifiedToken(ctx, tokens, proof, "Browser", true)
	})
	// The handler is the same as for the JSON refresh body.
	refresh := foundryhttp.DefineEndpoint(route("refresh"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), response).Handle(func(ctx context.Context, input RefreshInput) (IssuedUserToken, error) {
		return RefreshFromBody(ctx, tokens, input.Body)
	})
	// The cookie itself authenticates logout, so an expired access token cannot
	// leave the family live; without a usable cookie there is nothing to revoke.
	logout := foundryhttp.DefineEndpoint(route("logout"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie),
		foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204))).Handle(func(ctx context.Context, input RefreshLogoutInput) (foundryhttp.NoContent, error) {
		if refresh, ok := input.Body.RefreshToken.Get(); ok {
			if _, err := tokens.LogoutRefresh(ctx, refresh.Secret()); err != nil {
				return foundryhttp.NoContent{}, err
			}
		}
		return foundryhttp.NoContent{}, nil
	})
	return foundryhttp.NewRouter(login, refresh, logout)
}
