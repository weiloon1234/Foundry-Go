// Package httpvalidation exercises typed validation on existing generated DTOs.
package httpvalidation

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var Update = foundryhttp.ValidateBody(httpendpoints.Update,
	validation.Rules(validation.WithLabel(httpdto.UpdateUserValidationFields().Email, "Email address"), validation.Optional(validation.NonBlank[string]())),
	validation.Rules(validation.WithLabel(httpdto.UpdateUserValidationFields().Nickname, "Display name"), validation.Optional(validation.Nullable(validation.MinLength[string](2)))),
)

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
