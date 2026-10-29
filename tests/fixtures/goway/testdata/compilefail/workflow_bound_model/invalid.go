package invalid

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ex database.Executor, transport h.AuthenticatedEndpoint[h.NoPath, h.NoQuery, h.NoBody, models.User, h.NoContent]) {
	parent := modelbinding.ByKey(ex, orm.Keyed(models.Users.Query(), models.Users.ID), func(h.NoPath) model.ID[models.User] { return model.ID[models.User]{} })
	resources := modelbinding.ThroughRelation(ex, parent, models.UserRelations.Orders, models.Orders.ID, func(h.NoPath) model.ID[models.Order] { return model.ID[models.Order]{} })
	_ = modelbinding.BindAuthenticated(h.ForAuthenticatedBinding(transport), resources).Handle(func(context.Context, models.User, modelbinding.Input[h.NoPath, h.NoQuery, h.NoBody, models.Order]) (h.NoContent, error) {
		return h.NoContent{}, nil
	})
}
