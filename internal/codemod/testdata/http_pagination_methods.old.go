package consumer

import p "github.com/weiloon1234/Foundry-Go/http/pagination"
import h "github.com/weiloon1234/Foundry-Go/http"

var _ h.Scope

var page p.PageEndpoint[int, int, int, int, int]
var _ = page.Validate()
var _ = page.ID()
var _ = page.Method()
var _ = page.Pattern()
var _ = page.Description()
var _ = page.Within(1)
var _ = page.WithMiddleware(1)
var _ = page.WithLimits(1)
var _ = page.WithErrors(1)
var _ = page.WithPathValidation(1)
var _ = page.WithFiltersValidation(1)
var authPage p.AuthenticatedPageEndpoint[int, int, int, int, int, int]
var _ = authPage.WithScopes(1)
var _ = authPage.WithPermissions(1)
var _ = authPage.WithAuthorization(1)
var _ = authPage.Validate()
var _ = authPage.ID()
var _ = authPage.Method()
var _ = authPage.Pattern()
var _ = authPage.Description()
