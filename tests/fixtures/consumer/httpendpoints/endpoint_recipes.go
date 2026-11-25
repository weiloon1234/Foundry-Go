package httpendpoints

import (
	"context"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin client-parameters-input
// ClientParameters combines URL search with application headers.
type ClientParameters struct {
	Search  string
	Version string
	Device  value.Optional[string]
}

// doc-example:end client-parameters-input

// doc-example:begin http-endpoints-2
func clientParameters() foundryhttp.Query[ClientParameters] {
	parameters := foundryhttp.DefineQuery(
		foundryhttp.QueryParam("search", foundryhttp.StringQuery[string](), func(q *ClientParameters) *string { return &q.Search }),
		foundryhttp.HeaderParam("X-App-Version", foundryhttp.StringQuery[string](), func(q *ClientParameters) *string { return &q.Version }),
		foundryhttp.OptionalHeaderParam("X-Device-ID", foundryhttp.StringQuery[string](), func(q *ClientParameters) *value.Optional[string] { return &q.Device }),
	)

	return parameters
}

// doc-example:end http-endpoints-2

// doc-example:begin member-upsert-contracts

// Member carries the display name submitted to the upsert endpoint.
//
//foundry:dto http=body
type Member struct {
	Name string `json:"name"`
}

// Request keeps the upsert body concrete through the handler.
type Request = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Member]

// MemberStore reports whether an upsert inserted a new member.
type MemberStore interface {
	Upsert(context.Context, Member) (Member, bool, error)
}

// doc-example:end member-upsert-contracts

func registerUpsert(route foundryhttp.Route[foundryhttp.NoPath], members MemberStore) foundryhttp.RouteRegistration {
	// doc-example:begin http-endpoints-3
	var Upsert = foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), MemberBody(), foundryhttp.JSONResponses(MemberJSON(), 200, 201))
	registration := Upsert.Handle(func(ctx context.Context, in Request) (foundryhttp.Statused[Member], error) {
		member, created, err := members.Upsert(ctx, in.Body)
		if created {
			return foundryhttp.Statused[Member]{Status: 201, Value: member}, err
		}
		return foundryhttp.Statused[Member]{Value: member}, err
	})
	// doc-example:end http-endpoints-3
	return registration
}

type UserPath = httpkernel.UserPath
type NearbyInput = httpquery.NearbyInput
type UpdateUser = httpdto.UpdateUser
type UserResponse = httpdto.UserResponse

func updateURL(ctx context.Context, Update foundryhttp.Endpoint[UserPath, NearbyInput, UpdateUser, UserResponse], path UserPath, query NearbyInput) (string, error) {
	// doc-example:begin http-endpoints-4
	scoped := foundryhttp.ConfigureEndpoint(Update, foundryhttp.Within(foundryhttp.DefineScope("/api", "api")))
	return scoped.URL(ctx, path, query)
	// doc-example:end http-endpoints-4
}

// doc-example:begin order-location-contracts
// OrderReply carries the created resource identity.
type OrderReply struct{ ID model.ID[models.Order] }

// OrderPath binds the order identity used in a Location URL.
type OrderPath struct{ Order model.ID[models.Order] }

// doc-example:end order-location-contracts

func withLocation(endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, OrderReply], ShowOrder foundryhttp.Route[OrderPath]) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, OrderReply] {
	// doc-example:begin http-endpoints-5
	created := foundryhttp.EndpointHeaders(endpoint, func(_ context.Context, order OrderReply) ([]foundryhttp.ResponseHeader, error) {
		location, err := foundryhttp.RouteLocation(ShowOrder, OrderPath{Order: order.ID})
		return []foundryhttp.ResponseHeader{location}, err
	})
	// doc-example:end http-endpoints-5
	return created
}
