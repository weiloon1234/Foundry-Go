package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
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
	signed := Signed(endpoint, urlTestSigner(t, testkit.NewClock(urlTestTime)))
	registerEndpoint := func(r *foundation.Registrar, key foundation.Key[*Router], result RouteRegistration) error {
		return RegisterEndpoint(r, key, endpoint, func(foundation.Resolver) (RouteRegistration, error) { return result, nil })
	}
	registerSigned := func(r *foundation.Registrar, key foundation.Key[*Router], result RouteRegistration) error {
		return RegisterSignedEndpoint(r, key, signed, func(foundation.Resolver) (RouteRegistration, error) { return result, nil })
	}
	registerRaw := func(r *foundation.Registrar, key foundation.Key[*Router], result RouteRegistration) error {
		return RegisterRoute(r, key, route, func(foundation.Resolver) (RouteRegistration, error) { return result, nil })
	}
	bound := ConfigureEndpoint(endpoint, TableOperation(contract.TableOperationBinding{Table: "first", Kind: contract.TableQuery}))
	other := ConfigureEndpoint(endpoint, TableOperation(contract.TableOperationBinding{Table: "second", Kind: contract.TableQuery}))
	registerBound := func(r *foundation.Registrar, key foundation.Key[*Router], result RouteRegistration) error {
		return RegisterEndpoint(r, key, bound, func(foundation.Resolver) (RouteRegistration, error) { return result, nil })
	}
	cases := []struct {
		name     string
		register func(*foundation.Registrar, foundation.Key[*Router], RouteRegistration) error
		result   RouteRegistration
		valid    bool
	}{
		{"same", registerEndpoint, endpoint.Handle(handler), true},
		{"same-table", registerBound, bound.Handle(handler), true},
		{"changed-table", registerBound, other.Handle(handler), false},
		{"dropped-table", registerBound, endpoint.Handle(handler), false},
		{"added-table", registerEndpoint, bound.Handle(handler), false},
		{"added-signing", registerEndpoint, signed.Handle(handler), true},
		{"signed", registerSigned, signed.Handle(handler), true},
		{"missing-signing", registerSigned, endpoint.Handle(handler), false},
		{"raw-for-typed", registerEndpoint, HandleRaw(route, raw), false},
		{"typed-for-raw", registerRaw, endpoint.Handle(handler), false},
		{"body", registerEndpoint, wrongBody.Handle(func(context.Context, Input[NoPath, NoQuery, EndpointPatch]) (NoContent, error) {
			return NoContent{}, nil
		}), false},
		{"response", registerEndpoint, wrongResponse.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (EndpointReply, error) {
			return EndpointReply{}, nil
		}), false},
		{"method", registerRaw, HandleRaw(DefineRoute(RouteSpec{ID: "contract", Method: GET, Access: Public}, StaticPath("/contract")), raw), false},
		{"path", registerRaw, HandleRaw(DefineRoute(RouteSpec{ID: "contract", Method: POST, Access: Public}, StaticPath("/elsewhere")), raw), false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			key := foundation.NewKey[*Router]("contracts.router")
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "routes", OnRegister: func(r *foundation.Registrar) error {
				if err := RegisterRouter(r, key); err != nil {
					return err
				}
				return test.register(r, key, test.result)
			}}).Build(t.Context())
			if test.valid && err != nil || !test.valid && !errors.Is(err, fault.Invalid) {
				t.Fatal("incorrect contribution result", err)
			}
		})
	}
}
