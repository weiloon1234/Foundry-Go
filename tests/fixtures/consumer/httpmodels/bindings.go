// Package httpmodels verifies automatic route-model resolution in a consumer.
package httpmodels

import (
	"context"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ShowRequest = modelbinding.Input[httpkernel.UserPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User]

var Show = foundryhttp.DefineEndpoint(
	httpkernel.ShowUser,
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()),
)

type Service interface {
	Show(context.Context, ShowRequest) (httpdto.UserResponse, error)
}

func UserByID(db database.Executor) modelbinding.Resolver[httpkernel.UserPath, models.User] {
	return modelbinding.ByKey(db, orm.Keyed(models.Users.Query().Where(models.Users.Status.Eq(models.StatusActive)), models.Users.Key()),
		func(p httpkernel.UserPath) model.ID[models.User] { return p.User })
}

func Router(db database.Executor, service Service) (*foundryhttp.Router, error) {
	bound := modelbinding.Bind(Show, UserByID(db))
	return foundryhttp.NewRouter(bound.Handle(service.Show))
}

type Presenter struct{}

func (Presenter) Show(_ context.Context, in ShowRequest) (httpdto.UserResponse, error) {
	return httpdto.UserResponse{ID: in.Model.ID, Email: in.Model.Email, State: in.Model.Status}, nil
}

// Natural keys retain their declared type through the same typed Find API.
type CountryPath struct{ Country models.CountryCode }

func CountryByCode(db database.Executor) modelbinding.Resolver[CountryPath, models.Country] {
	return modelbinding.ByKey(db, orm.Keyed(models.Countries.Query(), models.Countries.Key()), func(p CountryPath) models.CountryCode { return p.Country })
}

// A child resolver supplies its domain ownership scope through ordinary typed
// predicates. An existing child outside the supplied parent resolves as absent.
type OrderPath struct {
	User  model.ID[models.User]
	Order model.ID[models.Order]
}

func OrderWithinUser(db database.Executor) modelbinding.Resolver[OrderPath, models.Order] {
	return modelbinding.Define(func(ctx context.Context, p OrderPath) (value.Optional[models.Order], error) {
		return orm.Keyed(models.Orders.Query().Where(models.Orders.BuyerID.Eq(p.User)), models.Orders.Key()).Find(ctx, db, p.Order)
	})
}
