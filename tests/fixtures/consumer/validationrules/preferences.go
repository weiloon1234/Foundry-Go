package validationrules

import "github.com/weiloon1234/Foundry-Go/validation"

type ReferenceCode string

//foundry:dto
type ProfilePreferences struct {
	Alias     string        `json:"alias"`
	Timezone  string        `json:"timezone"`
	Reference ReferenceCode `json:"reference"`
}

func PreferencesRules() validation.Rule[ProfilePreferences] {
	fields := ProfilePreferencesValidationFields()
	return validation.All(
		validation.Rules(fields.Alias, validation.NonBlank[string](), validation.AlphaNumeric[string]()),
		validation.Rules(fields.Timezone, validation.Timezone[string]()),
		validation.Rules(fields.Reference, validation.StartsWith[ReferenceCode]("00"), validation.Digits[ReferenceCode](), validation.MinLength[ReferenceCode](6), validation.MaxLength[ReferenceCode](6)),
	)
}
