package recovering

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type ResetInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ResetRequest]
type VerificationInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, VerificationRequest]

// CompletionRoutes accepts tokens only in JSON bodies. The browser opens a
// normal page first and explicitly submits the form; scanners/prefetch GETs do
// not consume a link. Neither completion operation creates a login credential.
// Domain password policy and transactional invalidation live in the reset binding.
func CompletionRoutes(reset *Reset, verification *Verification) (*foundryhttp.Router, error) {
	route := func(id foundryhttp.RouteID, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}

	changeEndpointRoute := route("recovery.reset", "/recovery/reset")
	changeEndpoint := foundryhttp.DefineEndpoint(changeEndpointRoute, foundryhttp.EmptyQuery(), ResetBody(), foundryhttp.EmptyResponse(204))

	change := foundryhttp.ConfigureEndpoint(changeEndpoint, foundryhttp.Use(foundryhttp.CredentialRequests(), foundryhttp.CSRF(foundryhttp.CSRFConfig{}))).Handle(func(ctx context.Context, input ResetInput) (foundryhttp.NoContent, error) {
		_, err := reset.Complete(ctx, input.Body.Token, input.Body.Password)
		return foundryhttp.NoContent{}, err
	})

	verifyEndpointRoute := route("recovery.verify", "/recovery/verify")
	verifyEndpoint := foundryhttp.DefineEndpoint(verifyEndpointRoute, foundryhttp.EmptyQuery(), VerificationBody(), foundryhttp.EmptyResponse(204))

	verify := foundryhttp.ConfigureEndpoint(verifyEndpoint, foundryhttp.Use(foundryhttp.CredentialRequests(), foundryhttp.CSRF(foundryhttp.CSRFConfig{}))).Handle(func(ctx context.Context, input VerificationInput) (foundryhttp.NoContent, error) {
		_, err := verification.Complete(ctx, input.Body.Token)
		return foundryhttp.NoContent{}, err
	})

	return foundryhttp.NewRouter(change, verify)
}
