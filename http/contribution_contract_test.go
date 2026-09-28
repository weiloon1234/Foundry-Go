package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestRouteFactoriesCannotSubstituteDeclaredTransport(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "contract", Method: POST, Access: Public}, StaticPath("/contract"))
	endpoint := DefineEndpoint(route, EmptyQuery(), EmptyBody(), EmptyResponse(204))
	handler := func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) { return NoContent{}, nil }
	raw := func(stdhttp.ResponseWriter, *stdhttp.Request, NoPath) {}
	wrongBody := DefineEndpoint(route, EmptyQuery(), JSONBody(endpointPatchJSON()), EmptyResponse(204))
	wrongResponse := DefineEndpoint(route, EmptyQuery(), EmptyBody(), JSONResponse(200, endpointReplyJSON()))
	signed := endpoint.Signed(urlTestSigner(t, testkit.NewClock(urlTestTime)))
	cases := []struct {
		name        string
		declaration routeDeclaration
		result      RouteRegistration
		valid       bool
	}{
		{"same", endpoint, endpoint.Handle(handler), true},
		{"added-signing", endpoint, signed.Handle(handler), true},
		{"signed", signed, signed.Handle(handler), true},
		{"missing-signing", signed, endpoint.Handle(handler), false},
		{"raw-for-typed", endpoint, route.HandleRaw(raw), false},
		{"typed-for-raw", route, endpoint.Handle(handler), false},
		{"body", endpoint, wrongBody.Handle(func(context.Context, Input[NoPath, NoQuery, EndpointPatch]) (NoContent, error) {
			return NoContent{}, nil
		}), false},
		{"response", endpoint, wrongResponse.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointReply, error) {
			return EndpointReply{}, nil
		}), false},
		{"method", route, DefineRoute(RouteSpec{ID: "contract", Method: GET, Access: Public}, StaticPath("/contract")).HandleRaw(raw), false},
		{"path", route, DefineRoute(RouteSpec{ID: "contract", Method: POST, Access: Public}, StaticPath("/elsewhere")).HandleRaw(raw), false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			key := foundation.NewKey[*Router]("contracts.router")
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "routes", OnRegister: func(r *foundation.Registrar) error {
				if err := RegisterRouter(r, key); err != nil {
					return err
				}
				return RegisterRoute(r, key, test.declaration, func(foundation.Resolver) (RouteRegistration, error) { return test.result, nil })
			}}).Build(t.Context())
			if test.valid && err != nil || !test.valid && !errors.Is(err, fault.Invalid) {
				t.Fatal("incorrect contribution result", err)
			}
		})
	}
}
