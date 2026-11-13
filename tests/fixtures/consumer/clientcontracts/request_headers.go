package clientcontracts

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// HeaderSequence is deliberately used only by headers, including in projections.
type HeaderSequence int64

type HeaderInput struct {
	Query    value.Optional[string]
	Version  string
	Sequence value.Optional[HeaderSequence]
}

var InspectHeaders = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "headers.inspect", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/headers")),
	foundryhttp.DefineQuery(
		foundryhttp.OptionalQueryParam("q", foundryhttp.StringQuery[string](), func(q *HeaderInput) *value.Optional[string] { return &q.Query }),
		foundryhttp.HeaderParam("X-App-Version", foundryhttp.StringQuery[string](), func(q *HeaderInput) *string { return &q.Version }),
		foundryhttp.OptionalHeaderParam("X-Sequence", foundryhttp.IntegerQuery[HeaderSequence](), func(q *HeaderInput) *value.Optional[HeaderSequence] { return &q.Sequence }),
	), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204),
)

func handleInspectHeaders(context.Context, foundryhttp.Input[foundryhttp.NoPath, HeaderInput, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
	return foundryhttp.NoContent{}, nil
}
