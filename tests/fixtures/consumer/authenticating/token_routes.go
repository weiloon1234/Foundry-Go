package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type RefreshInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]

// TokenRoutes demonstrates verified login, refresh and one-model hydration.
// verify is the trusted credential verifier; submitted identity is never proof.
// The response descriptor owns disclosure, TLS, cache and generated wire contracts.
// doc-example:begin native-token-delivery
func TokenRoutes(tokens *UserTokens, clock clock.Clock, verify func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error), scopes auth.AccessScopes[models.User]) (*foundryhttp.Router, error) {
	guard := tokens.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		return nil, err
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
	if err != nil {
		return nil, err
	}
	binding, err := foundryhttp.BindGuard(transport, guard)
	if err != nil {
		return nil, err
	}
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string, access foundryhttp.Access) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: access}, foundryhttp.StaticPath(path))
	}
	response := foundryhttp.TokenResponse[models.User, model.ID[models.User]](200, clock)

	loginRoute := route("api.login", foundryhttp.POST, "/login", foundryhttp.Public)

	login := foundryhttp.DefineEndpoint(loginRoute,
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response).Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (IssuedUserToken, error) {
		proof, err := verify(ctx)
		if err != nil {
			return IssuedUserToken{}, err
		}
		return StartVerifiedToken(ctx, tokens, proof, "API client", true, scopes)
	})

	refreshRoute := route("api.refresh", foundryhttp.POST, "/refresh", foundryhttp.Public)

	refresh := foundryhttp.DefineEndpoint(refreshRoute,
		foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenBody(), response).Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]) (IssuedUserToken, error) {
		return RefreshFromBody(ctx, tokens, input.Body)
	})
	profileEndpointRoute := route("api.profile", foundryhttp.GET, "/profile", foundryhttp.Guarded)
	profileEndpoint := foundryhttp.DefineEndpoint(
		profileEndpointRoute,
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	guardProfile := foundryhttp.Authenticated(profileEndpoint, binding, foundryhttp.AllOf(foundryhttp.Scopes(scopes)))

	profile := guardProfile.Handle(func(ctx context.Context, user models.User, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		// user is the concrete model; code that has only ctx gets it again from guard.Require(ctx).
		return foundryhttp.NoContent{}, nil
	})

	return foundryhttp.NewRouter(login, refresh, profile)
}

// doc-example:end native-token-delivery

// doc-example:begin refresh-from-body
func RefreshFromBody(ctx context.Context, tokens *UserTokens, request foundryhttp.RefreshTokenRequest) (IssuedUserToken, error) {
	return tokens.Refresh(ctx, request.RefreshToken.Secret())
}

// doc-example:end refresh-from-body
