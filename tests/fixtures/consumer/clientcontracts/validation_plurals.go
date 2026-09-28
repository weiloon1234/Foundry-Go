package clientcontracts

import (
	"context"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Exercise generated typed message arguments on a browser-supported rule. Tiny
// fractions must retain the English fallback if Intl would round their scale.
func pluralValidationRoutes() ([]foundryhttp.RouteRegistration, error) {
	var routes []foundryhttp.RouteRegistration
	for _, item := range []struct{ name, number string }{
		{"boundary", "0.00000000000000000001"},
		{"tiny", "0.000000000000000000001"},
		{"negative", "-0.000000000000000000001"},
	} {
		count, err := decimal.Parse(item.number)
		if err != nil {
			return nil, err
		}
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID("plural." + item.name), Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/plural/"+item.name))
		endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(contract.StringJSON[string]()), foundryhttp.JSONResponse(200, contract.StringJSON[string]())).WithBodyValidation(validation.WithTranslation(
			validation.NonBlank[string]().WithMessage("Enter a value."), localization.CartArgsMessage(), localization.CartArgs{Name: "Value", Count: count},
		))
		routes = append(routes, endpoint.Handle(func(_ context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, string]) (string, error) {
			return input.Body, nil
		}))
	}
	return routes, nil
}
