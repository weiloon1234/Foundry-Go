package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto
type PresenceInput struct {
	Label     value.Optional[string]                 `json:"label,omitzero"`
	Nickname  value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
	Count     value.Optional[int]                    `json:"count,omitzero"`
	Active    value.Optional[bool]                   `json:"active,omitzero"`
	Forbidden value.Optional[value.Nullable[string]] `json:"forbidden,omitzero"`
	Tags      value.Optional[[]string]               `json:"tags,omitzero"`
}

func PresenceRules() validation.Rule[PresenceInput] {
	fields := PresenceInputValidationFields()
	return validation.All(
		validation.Rules(fields.Label, validation.Required(validation.MaxLength[string](40))),
		validation.Rules(fields.Nickname, validation.RequiredNullable(validation.MinLength[string](2))),
		validation.Rules(fields.Count, validation.Required(validation.Min(0))),
		validation.Rules(fields.Active, validation.Required[bool]()),
		validation.Rules(fields.Forbidden, validation.ProhibitedNullable[string]()),
		validation.Rules(fields.Tags, validation.Required(validation.Each[[]string](validation.NonBlank[string]()))),
	)
}

// Any supplied, nonempty trigger requires a label. Zero and false activate the
// rule, whereas an omitted trigger does not. All references are generated fields.
func RequiredWithRules() validation.Rule[PresenceInput] {
	fields := PresenceInputValidationFields()
	noTriggers := validation.All(
		validation.Rules(fields.Count, validation.Prohibited[int]()),
		validation.Rules(fields.Active, validation.Prohibited[bool]()),
	)
	return validation.Unless(noTriggers, validation.Rules(fields.Label, validation.Required[string]()))
}
