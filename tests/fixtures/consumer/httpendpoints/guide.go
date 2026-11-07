package httpendpoints

import (
	"context"
	"strings"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin compose-endpoint
type UpdateService interface {
	UpdateValues(context.Context, model.ID[models.User], httpdto.UpdateUser) (httpdto.UserResponse, error)
}

func GuideRouter(service UpdateService) (*foundryhttp.Router, error) {
	api := foundryhttp.DefineScope("/api", "api")
	users := foundryhttp.DefineScope("/members", "members").Within(api)
	route := foundryhttp.ConfigureRoute(foundryhttp.DefineRoute(
		foundryhttp.RouteSpec{ID: "update", Method: foundryhttp.PATCH, Access: foundryhttp.Public},
		httpkernel.UserPathDescriptor(),
	), foundryhttp.Within(users))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(),
		foundryhttp.JSONBody(httpdto.UpdateUserJSON()), foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()))
	endpoint = foundryhttp.ValidateBody(endpoint,
		validation.Rules(httpdto.UpdateUserValidationFields().Email, validation.Optional(validation.NonBlank[string]())))
	endpoint = foundryhttp.PrepareEndpoint(endpoint, func(_ context.Context, in foundryhttp.Input[httpkernel.UserPath, foundryhttp.NoQuery, httpdto.UpdateUser]) (foundryhttp.NoQuery, httpdto.UpdateUser, error) {
		if email, present := in.Body.Email.Get(); present {
			in.Body.Email = value.Set(strings.TrimSpace(email))
		}
		return in.Query, in.Body, nil
	})
	return foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, in foundryhttp.Input[httpkernel.UserPath, foundryhttp.NoQuery, httpdto.UpdateUser]) (httpdto.UserResponse, error) {
		return service.UpdateValues(ctx, in.Path.User, in.Body)
	}))
} // doc-example:end compose-endpoint
