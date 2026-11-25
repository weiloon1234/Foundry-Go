// Package validationrules verifies generated fields with reusable typed rules.
package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin registration-input

//foundry:enum
type OrganizationKind string

const (
	Personal OrganizationKind = "personal"
	Business OrganizationKind = "business"
)

//foundry:dto
type Registration struct {
	Kind    OrganizationKind       `json:"kind"`
	Email   string                 `json:"email"`
	Company value.Optional[string] `json:"company,omitzero"`
	Tags    []string               `json:"tags"`
}

// doc-example:end registration-input

// doc-example:begin conditional-registration-rules
func registrationRules() validation.Rule[Registration] {
	fields := RegistrationValidationFields()
	business := validation.Rules(fields.Kind, validation.OneOf(Business))
	return validation.All(
		validation.Rules(fields.Kind, validation.Enum(Business.EnumDescriptor())),
		validation.Rules(fields.Email, validation.Email[string]()),
		validation.When(business, validation.Rules(fields.Company, validation.Bail(validation.Present[string](), validation.Optional(validation.NonBlank[string]())))),
		validation.Unless(business, validation.Rules(fields.Company, validation.Absent[string]())),
		validation.Rules(fields.Tags, validation.MinItems[[]string](1), validation.MaxItems[[]string](8), validation.Distinct[[]string](), validation.Each[[]string](validation.NonBlank[string]())),
	)
}

// doc-example:end conditional-registration-rules

// Rules exposes the conditional registration rules to consumer checks.
func Rules() validation.Rule[Registration] {
	return registrationRules()
}
