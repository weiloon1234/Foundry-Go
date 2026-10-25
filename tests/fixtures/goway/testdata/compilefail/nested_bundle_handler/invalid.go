package invalid

import (
	"context"
	"foundry.test/goway/models"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

func bad(endpoint h.Endpoint[h.NoPath, h.NoQuery, h.NoBody, h.NoContent], resolver modelbinding.Resolver[h.NoPath, modelbinding.Models[models.User, models.Order]]) {
	_ = modelbinding.Bind(endpoint, resolver).Handle(func(context.Context, modelbinding.Input[h.NoPath, h.NoQuery, h.NoBody, models.User]) (h.NoContent, error) {
		return h.NoContent{}, nil
	})
}
