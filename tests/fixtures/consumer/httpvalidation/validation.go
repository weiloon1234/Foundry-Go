// Package httpvalidation exercises typed validation on existing generated DTOs.
package httpvalidation

import (
	dto "foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	routes "foundry.test/consumer/httpkernel"
	filters "foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin validate-http-body
func validatedUpdate(endpoint foundryhttp.Endpoint[routes.UserPath, filters.NearbyInput, dto.UpdateUser, dto.UserResponse]) foundryhttp.Endpoint[routes.UserPath, filters.NearbyInput, dto.UpdateUser, dto.UserResponse] {
	return foundryhttp.ValidateBody(endpoint,
		validation.Rules(validation.WithLabel(dto.UpdateUserValidationFields().Email, "Email address"), validation.Optional(validation.NonBlank[string]())),
		validation.Rules(validation.WithLabel(dto.UpdateUserValidationFields().Nickname, "Display name"), validation.Optional(validation.Nullable(validation.MinLength[string](2)))),
	)
}

// doc-example:end validate-http-body

// Update applies the application field labels to its declared endpoint.
var Update = validatedUpdate(httpendpoints.Update)

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
