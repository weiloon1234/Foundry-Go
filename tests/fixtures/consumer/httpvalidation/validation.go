// Package httpvalidation exercises typed validation on existing generated DTOs.
package httpvalidation

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var Update = httpendpoints.Update.WithBodyValidation(
	httpdto.UpdateUserValidationFields().Email.WithLabel("Email address").Rules(validation.Optional(validation.NonBlank[string]())),
	httpdto.UpdateUserValidationFields().Nickname.WithLabel("Display name").Rules(validation.Optional(validation.Nullable(validation.MinLength[string](2)))),
)

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
