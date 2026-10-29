package consumer

import p "github.com/weiloon1234/Foundry-Go/http/pagination"
import h "github.com/weiloon1234/Foundry-Go/http"

var _ h.Scope

var page p.PageEndpoint[int, int, int, int, int]
var _ = p.Validate(page)
var _ = p.EndpointID(page)
var _ = p.EndpointMethod(page)
var _ = p.EndpointPattern(page)
var _ = p.Inspect(page)
var _ = p.Configure(page, h.Within(1))
var _ = p.Configure(page, h.Use(1))
var _ = p.Configure(page, h.Limits(1))
var _ = p.Configure(page, h.Errors(1))
var _ = p.ValidatePath(page, 1)
var _ = p.ValidateFilters(page, 1)
var authPage p.AuthenticatedPageEndpoint[int, int, int, int, int, int]
var _ = p.ConfigureAuthenticated(authPage, h.Scopes(1))
var _ = p.ConfigureAuthenticated(authPage, h.Permissions(1))
var _ = p.AuthorizeActor(authPage, 1)
var _ = p.ValidateAuthenticated(authPage)
var _ = p.AuthenticatedEndpointID(authPage)
var _ = p.AuthenticatedEndpointMethod(authPage)
var _ = p.AuthenticatedEndpointPattern(authPage)
var _ = p.InspectAuthenticated(authPage)
