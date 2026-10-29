package pagination

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

func Configure[P, F, W, Page, Response any](endpoint pageEndpoint[P, F, W, Page, Response], options ...foundryhttp.EndpointOption) pageEndpoint[P, F, W, Page, Response] {
	endpoint.transport = foundryhttp.ConfigureEndpoint(endpoint.transport, options...)
	return endpoint
}
func ConfigureAuthenticated[P, F, W, A, Page, Response any](endpoint authenticatedPageEndpoint[P, F, W, A, Page, Response], options ...foundryhttp.GuardOption[A]) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	endpoint.transport = foundryhttp.ConfigureAuthenticated(endpoint.transport, options...)
	return endpoint
}
