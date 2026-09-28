package recovering

import (
	"context"
	"net/netip"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type LinkInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, LinkRequest]

// LinkRoutes deliberately acknowledges requests without returning account/link
// information. The fixture uses exact stored email spelling in its lookup. A
// case-insensitive domain must canonicalize before both quota and lookup.
// Ingress quota is shared across these two routes; recipient quotas belong to
// their separately configured framework requesters.
func LinkRoutes(reset *ResetRequests, verification *VerificationRequests, ingress ratelimit.Limiter[netip.Addr]) (*foundryhttp.Router, error) {
	endpoint := func(id foundryhttp.RouteID, path string) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, LinkRequest, foundryhttp.NoContent] {
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
		return foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(LinkRequestJSON()), foundryhttp.EmptyResponse(204)).
			WithMiddleware(foundryhttp.CredentialRequests(), foundryhttp.CSRF(foundryhttp.CSRFConfig{}), foundryhttp.RateLimitByIP(ingress)).
			WithBodyValidation(LinkRequestValidationFields().Email.Rules(validation.Email[string]()))
	}
	return foundryhttp.NewRouter(
		endpoint("recovery.request-reset", "/recovery/request-reset").Handle(func(ctx context.Context, input LinkInput) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, reset.Request(ctx, input.Body.Email)
		}),
		endpoint("recovery.request-verification", "/recovery/request-verification").Handle(func(ctx context.Context, input LinkInput) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, verification.Request(ctx, input.Body.Email)
		}),
	)
}
