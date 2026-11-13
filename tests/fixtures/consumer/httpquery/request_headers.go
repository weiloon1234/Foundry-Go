package httpquery

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RequestParameters keeps URL inputs and application headers in one typed input.
//
//foundry:query
type RequestParameters struct {
	Search     value.Optional[string] `query:"q"`
	AppVersion string                 `header:"X-App-Version"`
	DeviceID   value.Optional[string] `header:"X-Device-ID"`
}

func RequestHeadersEndpoint() foundryhttp.Endpoint[foundryhttp.NoPath, RequestParameters, foundryhttp.NoBody, foundryhttp.NoContent] {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "headers.inspect", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, foundryhttp.StaticPath("/headers"))
	return foundryhttp.DefineEndpoint(route, RequestParametersDescriptor(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
}
