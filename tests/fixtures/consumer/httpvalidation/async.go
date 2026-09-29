package httpvalidation

import (
	"context"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
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

// OwnEmailRouter is declared once. The trusted route key of the user being
// updated is provided per request through a validation slot, so the lookup
// can exclude that user's own address while the rule selects the body field.
func OwnEmailRouter(service httpendpoints.Service, current validation.Slot[model.ID[models.User]], emails validation.Lookup[string]) (*foundryhttp.Router, error) {
	body := validation.DefineField("body", func(input httpendpoints.UpdateRequest) httpdto.UpdateUser { return input.Body })
	fields := httpdto.UpdateUserValidationFields()
	endpoint := httpendpoints.Update.WithValidation(validation.Provide(current,
		func(_ context.Context, input httpendpoints.UpdateRequest) (model.ID[models.User], error) {
			return input.Path.User, nil
		},
		body.Rules(fields.Email.Rules(validation.Optional(validation.Requires(current, validation.Unique(emails))))),
	))
	return foundryhttp.NewRouter(endpoint.Handle(service.Update))
}
