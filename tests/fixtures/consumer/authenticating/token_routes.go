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
func TokenRoutes(tokens *UserTokens, clock clock.Clock, verify VerifiedLogin) (*foundryhttp.Router, error) {
	guard := tokens.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		return nil, err
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
	if err != nil {
		return nil, err
	}
	scopes, err := OrderReadScopes()
	if err != nil {
		return nil, err
	}
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string, access foundryhttp.Access) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: access}, foundryhttp.StaticPath(path))
	}
	response := foundryhttp.TokenResponse[models.User, model.ID[models.User]](200, clock)
	login := foundryhttp.DefineEndpoint(route("api.login", foundryhttp.POST, "/login", foundryhttp.Public),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response).Handle(func(ctx context.Context, _ ProfileInput) (IssuedUserToken, error) {
		proof, err := verify(ctx)
		if err != nil {
			return IssuedUserToken{}, err
		}
		return StartVerifiedToken(ctx, tokens, proof, "API client", true)
	})
	refresh := foundryhttp.DefineEndpoint(route("api.refresh", foundryhttp.POST, "/refresh", foundryhttp.Public),
		foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenBody(), response).Handle(func(ctx context.Context, input RefreshInput) (IssuedUserToken, error) {
		return RefreshFromBody(ctx, tokens, input.Body)
	})
	profile := foundryhttp.RequireAuthentication(foundryhttp.DefineEndpoint(
		route("api.profile", foundryhttp.GET, "/profile", foundryhttp.Guarded),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204)), transport, guard).
		WithScopes(scopes).Handle(func(ctx context.Context, user models.User, _ ProfileInput) (foundryhttp.NoContent, error) {
		// Policies and additional guard calls reuse this concrete model.
		_, err := guard.Require(ctx)
		return foundryhttp.NoContent{}, err
	})
	return foundryhttp.NewRouter(login, refresh, profile)
}

func RefreshFromBody(ctx context.Context, tokens *UserTokens, request foundryhttp.RefreshTokenRequest) (IssuedUserToken, error) {
	return tokens.Refresh(ctx, request.RefreshToken.Secret())
}
