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
		fields.Label.Rules(validation.Required(validation.MaxLength[string](40))),
		fields.Nickname.Rules(validation.RequiredNullable(validation.MinLength[string](2))),
		fields.Count.Rules(validation.Required(validation.Min(0))),
		fields.Active.Rules(validation.Required[bool]()),
		fields.Forbidden.Rules(validation.ProhibitedNullable[string]()),
		fields.Tags.Rules(validation.Required(validation.Each[[]string](validation.NonBlank[string]()))),
	)
}

// Any supplied, nonempty trigger requires a label. Zero and false activate the
// rule, whereas an omitted trigger does not. All references are generated fields.
func RequiredWithRules() validation.Rule[PresenceInput] {
	fields := PresenceInputValidationFields()
	noTriggers := validation.All(
		fields.Count.Rules(validation.Prohibited[int]()),
		fields.Active.Rules(validation.Prohibited[bool]()),
	)
	return validation.Unless(noTriggers, fields.Label.Rules(validation.Required[string]()))
}
