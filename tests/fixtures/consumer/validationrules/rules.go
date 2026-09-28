// Package validationrules verifies generated fields with reusable typed rules.
package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

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
	Website value.Optional[string] `json:"website,omitzero"`
}

func Rules() validation.Rule[Registration] {
	fields := RegistrationValidationFields()
	business := fields.Kind.Rules(validation.OneOf(Business))
	return validation.All(
		fields.Kind.Rules(validation.Enum(Business.EnumDescriptor())),
		fields.Email.Rules(validation.Email[string]()),
		validation.When(business, fields.Company.Rules(validation.Bail(validation.Present[string](), validation.Optional(validation.NonBlank[string]())))),
		validation.Unless(business, fields.Company.Rules(validation.Absent[string]())),
		fields.Tags.Rules(validation.MinItems[[]string](1), validation.MaxItems[[]string](8), validation.Distinct[[]string](), validation.Each[[]string](validation.NonBlank[string]())),
		fields.Website.Rules(validation.Optional(validation.URL[string]())),
	)
}
