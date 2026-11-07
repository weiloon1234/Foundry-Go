// Package codecs demonstrates concrete application value boundaries.
package codecs

import (
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/phone"
	"github.com/weiloon1234/Foundry-Go/validation"
	phonevalidation "github.com/weiloon1234/Foundry-Go/validation/phone"
)

// doc-example:begin normalize-phone
func NormalizePhone(country countries.Code, input string, options phone.Options) (string, error) {
	number, err := phone.Parse(country, input, options)
	if err != nil {
		return "", err
	}
	return number.String(), nil
}

// doc-example:end normalize-phone

// doc-example:begin validate-phone
type Contact struct {
	Country countries.Code `json:"country"`
	Number  string         `json:"number"`
}

func ContactRule(options phone.Options) validation.Rule[Contact] {
	country := validation.DefineField("country", func(in Contact) countries.Code { return in.Country })
	number := validation.DefineField("number", func(in Contact) string { return in.Number })
	return phonevalidation.For(country, number, options)
}

func NationalPhoneRule(country countries.Code, options phone.Options) validation.Rule[string] {
	return phonevalidation.Number[string](country, options)
}

// doc-example:end validate-phone

// doc-example:begin phone-messages
func PhoneMessages() []i18n.MessageDefinition {
	return append(validation.MessageDefinitions(), phonevalidation.MessageDefinitions()...)
}

// doc-example:end phone-messages
