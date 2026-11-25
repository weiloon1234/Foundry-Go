package httpkernel

import (
	"foundry.test/consumer/models"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin generated-user-url
func userURL(user models.User) (string, error) {
	ShowUser := foundryhttp.DefineRoute(
		foundryhttp.RouteSpec{ID: "users.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
		UserPathDescriptor(),
	)
	ShowUser = foundryhttp.ConfigureRoute(ShowUser, foundryhttp.Within(foundryhttp.DefineScope("/api", "api")))
	location, err := foundryhttp.RouteURL(ShowUser, UserPath{User: user.ID})
	return location, err
}

// doc-example:end generated-user-url
