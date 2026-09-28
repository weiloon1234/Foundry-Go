package httpendpoints

import (
	"context"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// SeatUnavailable is the public failure declaration reused by transport and
// domain behavior. Internal causes remain ordinary Go errors.
var SeatUnavailable = foundryhttp.DefineError("seats.unavailable", 409, "The selected seat is no longer available.")

var Reserve = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "seats.reserve", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/seats/reserve")),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204),
).WithErrors(SeatUnavailable)

// ReservationService defines only the domain operation.
type ReservationService interface{ Reserve(context.Context) error }

func ReservationRouter(service ReservationService) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Reserve.Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, service.Reserve(ctx)
	}))
}
