// Package payment declares the payment alternatives accepted by checkout.
package payment

// doc-example:begin payment-variants

// CardDTO carries a payment-provider token.
//
//foundry:dto
type CardDTO struct {
	Token string `json:"token"`
}

// BankDTO carries the bank transfer reference.
//
//foundry:dto
type BankDTO struct {
	Reference string `json:"reference"`
}

// PaymentMethodVariants closes the set of supported checkout methods.
//
//foundry:union name=PaymentMethod discriminator=kind
type PaymentMethodVariants struct {
	Card CardDTO `union:"card"`
	Bank BankDTO `union:"bank_transfer"`
}

// doc-example:end payment-variants

func paymentLabel(useToken func(string)) (string, error) {
	// doc-example:begin tagged-unions-2
	method, err := PaymentMethodFromCard(CardDTO{Token: "provider-token"})
	if err != nil {
		return "", err
	}
	card, selected := method.Card()
	if selected {
		useToken(card.Token)
	}
	label, err := MatchPaymentMethod(method,
		func(card CardDTO) (string, error) { return card.Token, nil },
		func(bank BankDTO) (string, error) { return bank.Reference, nil },
	)
	// doc-example:end tagged-unions-2
	return label, err
}
