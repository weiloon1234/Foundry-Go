// Package httpendpoints verifies typed transport composition in a consumer.
package httpendpoints

import (
	"context"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// UpdateRequest retains generated declarations from all three input sources.
type UpdateRequest = foundryhttp.Input[httpkernel.UserPath, httpquery.NearbyInput, httpdto.UpdateUser]

var Update = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "users.update", Method: foundryhttp.PATCH, Access: foundryhttp.Public}, httpkernel.UserPathDescriptor()),
	httpquery.NearbyInputDescriptor(),
	foundryhttp.JSONBody(httpdto.UpdateUserJSON()),
	foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()),
)

// Service is a domain boundary: transport parsing stays inside Foundry.
type Service interface {
	Update(context.Context, UpdateRequest) (httpdto.UserResponse, error)
}

func Router(service Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
