package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
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
func BrowserTokenRoutes(tokens *UserTokens, clock clock.Clock, verify VerifiedLogin, cookie foundryhttp.RefreshCookie, name string, scopes auth.AccessScopes[models.User]) (*foundryhttp.Router, error) {
	login, refresh := browserTokenDelivery(tokens, clock, verify, cookie, name, scopes)
	logout := browserTokenLogout(tokens, cookie, name)
	return foundryhttp.NewRouter(login, refresh, logout)
}

// doc-example:begin browser-token-delivery
func browserTokenDelivery(tokens *UserTokens, clock clock.Clock, verify func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error), cookie foundryhttp.RefreshCookie, name string, scopes auth.AccessScopes[models.User]) (foundryhttp.RouteRegistration, foundryhttp.RouteRegistration) {
	route := func(id string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID(name + "." + id), Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+name+"/"+id))
	}
	response := foundryhttp.TokenCookieResponse[models.User, model.ID[models.User]](cookie, 200, clock)

	loginRoute := route("login")

	login := foundryhttp.DefineEndpoint(loginRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response).Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (IssuedUserToken, error) {
		proof, err := verify(ctx)
		if err != nil {
			return IssuedUserToken{}, err
		}
		return StartVerifiedToken(ctx, tokens, proof, "Browser", true, scopes)
	})
	// The handler is the same as for the JSON refresh body.
	refreshRoute := route("refresh")

	refresh := foundryhttp.DefineEndpoint(refreshRoute, foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), response).Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]) (IssuedUserToken, error) {
		return RefreshFromBody(ctx, tokens, input.Body)
	})

	return login, refresh
}

// doc-example:end browser-token-delivery

// doc-example:begin browser-token-logout
func browserTokenLogout(tokens *UserTokens, cookie foundryhttp.RefreshCookie, name string) foundryhttp.RouteRegistration {
	logoutRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID(name + ".logout"), Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+name+"/logout"))

	logout := foundryhttp.DefineEndpoint(logoutRoute, foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie),
		foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204))).Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshCookieLogoutRequest]) (foundryhttp.NoContent, error) {
		if refresh, ok := input.Body.RefreshToken.Get(); ok {
			if _, err := tokens.LogoutRefresh(ctx, refresh.Secret()); err != nil {
				return foundryhttp.NoContent{}, err
			}
		}
		return foundryhttp.NoContent{}, nil
	})

	return logout
}

// doc-example:end browser-token-logout
