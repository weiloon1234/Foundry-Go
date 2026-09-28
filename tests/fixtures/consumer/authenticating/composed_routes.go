package authenticating

import (
	"context"
	stdhttp "net/http"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

type OrderPath struct{ Order model.ID[models.Order] }
type OrderInput = modelbinding.Input[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.Order]
type OrderEndpoint = foundryhttp.AuthenticatedEndpoint[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent]
type SignedOrderEndpoint = foundryhttp.SignedAuthenticatedEndpoint[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent]
type OrderTransport = foundryhttp.AuthenticatedTransport[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent]

var showOrder = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "orders.show", Method: foundryhttp.GET, Access: foundryhttp.Guarded},
		foundryhttp.DefinePath("/orders/{order}", foundryhttp.Param("order", foundryhttp.ModelIDPath[models.Order](), func(p *OrderPath) *model.ID[models.Order] { return &p.Order }))),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204),
)

func Orders(transport *foundryhttp.Authentication, guard auth.Guard[models.User]) OrderEndpoint {
	return foundryhttp.RequireAuthentication(showOrder, transport, guard).WithPermissions(ViewAccount)
}
func SignedOrders(endpoint OrderEndpoint, signer foundryhttp.URLSigner) SignedOrderEndpoint {
	return endpoint.Signed(signer)
}
func OrderResource(db database.Executor) modelbinding.Resolver[OrderPath, models.Order] {
	return modelbinding.ByKey(db, models.QueryOrders(), func(path OrderPath) model.ID[models.Order] { return path.Order })
}

// BoundOrder composes subject admission and one typed resource lookup, then the
// existing resource policy before domain behavior. A signature does not authorize
// another member's order. The same binding accepts an ordinary or signed endpoint.
func BoundOrder(endpoint OrderTransport, resolver modelbinding.Resolver[OrderPath, models.Order], guard auth.Guard[models.User], read func(context.Context, models.User, models.Order) error) foundryhttp.RouteRegistration {
	if read == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "order route requires domain behavior"))
	}
	return modelbinding.BindAuthenticated(endpoint, resolver).Handle(func(ctx context.Context, subject models.User, input OrderInput) (foundryhttp.NoContent, error) {
		if err := ReadOrder.Authorize(ctx, guard, input.Model); err != nil {
			return foundryhttp.NoContent{}, err
		}
		return foundryhttp.NoContent{}, read(ctx, subject, input.Model)
	})
}

var nativeProfile = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "profile.native", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/profile/native"))

func NativeProfile(transport *foundryhttp.Authentication, guard auth.Guard[models.User]) foundryhttp.AuthenticatedRoute[foundryhttp.NoPath, models.User] {
	return foundryhttp.RequireRouteAuthentication(nativeProfile, transport, guard).WithPermissions(ViewAccount)
}
func NativeProfileHandler(route foundryhttp.AuthenticatedRoute[foundryhttp.NoPath, models.User], handler func(stdhttp.ResponseWriter, *stdhttp.Request, models.User, foundryhttp.NoPath)) foundryhttp.RouteRegistration {
	return route.HandleRaw(handler)
}
