package httpvalidation

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// AsyncRouter uses the same request lifecycle and 422 mapping as local rules.
// The lookup may be a scoped database query or an application API adapter.
func AsyncRouter(service httpendpoints.Service, lookup validation.Lookup[string]) (*foundryhttp.Router, error) {
	fields := httpdto.UpdateUserValidationFields()
	endpoint := httpendpoints.Update.WithBodyValidation(validation.Parallel(
		fields.Email.WithLabel("Email address").Rules(validation.Optional(validation.Bail(validation.Email[string](), validation.Unique(lookup)))),
		fields.Nickname.Rules(validation.Optional(validation.Nullable(validation.Lowercase[string]()))),
	))
	return foundryhttp.NewRouter(endpoint.Handle(service.Update))
}
