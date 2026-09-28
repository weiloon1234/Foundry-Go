package http

import (
	"context"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestEndpointScalarDescriptionsAreOwned(t *testing.T) {
	type routePath struct{ Code string }
	type routeQuery struct{ Count int16 }
	endpoint := DefineEndpoint(
		DefineRoute(RouteSpec{ID: "scalars.show", Method: GET, Access: Public}, DefinePath("/{code}", Param("code", StringPath[string](), func(p *routePath) *string { return &p.Code }))),
		DefineQuery(QueryParam("count", IntegerQuery[int16](), func(q *routeQuery) *int16 { return &q.Count })),
		EmptyBody(), EmptyResponse(204))
	router, err := NewRouter(endpoint.Handle(func(context.Context, Input[routePath, routeQuery, NoBody]) (NoContent, error) {
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	first := router.Endpoints()
	if len(first) != 1 || len(first[0].Path) != 1 || first[0].Path[0].Scalar.Value.Kind != contract.StringKind || first[0].Query[0].Scalar.Value.Bits != 16 {
		t.Fatalf("endpoint scalar metadata: %+v", first)
	}
	direct, err := endpoint.Description()
	if err != nil || !reflect.DeepEqual(first[0], direct) {
		t.Fatal("registered and direct descriptions disagree", err)
	}
	first[0].Path[0].Scalar.Value.ID = "changed"
	first[0].Query[0].Scalar.Value.Bits = 64
	again := router.Endpoints()
	if again[0].Path[0].Scalar.Value.ID == "changed" || again[0].Query[0].Scalar.Value.Bits != 16 {
		t.Fatal("endpoint metadata shared mutable fields")
	}
}
