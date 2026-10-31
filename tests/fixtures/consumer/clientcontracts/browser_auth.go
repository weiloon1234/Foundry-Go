package clientcontracts

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// BrowserLogin proves that the auth descriptor uses the DTO's declared JSON key.
//
//foundry:dto
type BrowserLogin struct {
	Email    string `json:"login_id" client:"kind=email"`
	Password string `json:"password" client:"kind=password"`
}

// browserAuthRoutes registers the real browser cookie adapters and guarded
// profile without requiring a token store. This contract fixture has no issuing
// authority, so login and refresh deny credentials. The authenticating consumer
// exercises issuance and rotation against its PostgreSQL token store.
func browserAuthRoutes(transport *foundryhttp.Authentication, guard auth.Guard[Account]) []foundryhttp.RouteRegistration {
	cookie := foundryhttp.DefineRefreshCookie("__Host-refresh-client", foundryhttp.CSRFConfig{})
	route := func(id string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID("browser." + id), Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/browser/"+id))
	}
	response := foundryhttp.TokenCookieResponse[Account, int64](cookie, 200, clock.System{})
	login := foundryhttp.DefineEndpoint(route("login"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(BrowserLoginJSON()), response)
	refresh := foundryhttp.DefineEndpoint(route("refresh"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), response)
	logout := foundryhttp.DefineEndpoint(route("logout"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204)))
	profile := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "browser.profile", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/browser/profile")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, MemberJSON()),
	)
	return []foundryhttp.RouteRegistration{
		login.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, BrowserLogin]) (token.Issued[Account, int64], error) {
			return token.Issued[Account, int64]{}, auth.Unauthenticated
		}),
		refresh.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]) (token.Issued[Account, int64], error) {
			return token.Issued[Account, int64]{}, auth.Unauthenticated
		}),
		logout.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshCookieLogoutRequest]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		foundryhttp.RequireAuthentication(profile, transport, guard).Handle(func(_ context.Context, actor Account, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (Member, error) {
			return Member{Display: actor.Display}, nil
		}),
	}
}
