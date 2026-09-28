package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func bad(endpoint authenticating.OptionalProfileEndpoint) {
	_ = endpoint.Handle(func(context.Context, models.User, authenticating.ProfileInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
}
