// Package unions proves the public closed-union API without application infrastructure.
package unions

import (
	"context"

	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto
type CardDTO struct {
	Token    string   `json:"token"`
	Sequence int64    `json:"sequence"`
	Labels   []string `json:"labels,omitempty"`
}

//foundry:dto
type BankDTO struct {
	Reference string `json:"reference"`
}

// PaymentMethodVariants lists typed payloads, not independently authored schemas.
//
//foundry:union name=PaymentMethod discriminator=kind
type PaymentMethodVariants struct {
	Card    CardDTO                      `union:"card"`
	Bank    BankDTO                      `union:"bank_transfer"`
	Wrapped genericdto.Envelope[CardDTO] `union:"wrapped"`
}

//foundry:union name=Delivery discriminator=status
type DeliveryVariants struct {
	Sent BankDTO `union:"sent"`
}

//foundry:dto
type PaymentRequest struct {
	Method PaymentMethod                                 `json:"method"`
	More   []PaymentMethod                               `json:"more,omitempty"`
	Maybe  value.Optional[value.Nullable[PaymentMethod]] `json:"maybe,omitzero"`
}

var Echo = http.DefineEndpoint(
	http.DefineRoute(http.RouteSpec{ID: "unions.echo", Method: http.POST, Access: http.Public}, http.StaticPath("/unions/echo")),
	http.EmptyQuery(), http.JSONBody(PaymentRequestJSON()), http.JSONResponse(200, genericdto.EnvelopeJSON(PaymentRequestJSON())),
)

type EchoInput = http.Input[http.NoPath, http.NoQuery, PaymentRequest]

func Handle(_ context.Context, input EchoInput) (genericdto.Envelope[PaymentRequest], error) {
	return genericdto.Envelope[PaymentRequest]{Data: input.Body, Trace: "union"}, nil
}
func Router() (*http.Router, error) { return http.NewRouter(Echo.Handle(Handle)) }
func CardToken(input PaymentMethod) string {
	card, ok := input.Card()
	if !ok {
		return ""
	}
	return card.Token
}
func Describe(input PaymentMethod) (string, error) {
	return MatchPaymentMethod(input,
		func(card CardDTO) (string, error) { return card.Token, nil },
		func(bank BankDTO) (string, error) { return bank.Reference, nil },
		func(wrapped genericdto.Envelope[CardDTO]) (string, error) { return wrapped.Data.Token, nil },
	)
}

func NewCard(token string) (PaymentMethod, error) {
	return PaymentMethodFromCard(CardDTO{Token: token})
}
