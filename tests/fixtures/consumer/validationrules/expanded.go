package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// RegistrationChecks runs independent fields concurrently, retaining cheap
// checks before IO in each field. The injected lookup must support concurrency.
func RegistrationChecks(addresses validation.Lookup[string]) validation.Rule[Registration] {
	fields := RegistrationValidationFields()
	return validation.Parallel(
		validation.Rules(fields.Email, validation.Bail(validation.Email[string](), validation.MaxLength[string](254), validation.Unique(addresses))),
		validation.Rules(fields.Tags, validation.Bail(validation.ItemsBetween[[]string](1, 8), validation.Distinct[[]string](), validation.Each[[]string](validation.AlphaDash[string]()))),
		validation.When(validation.Rules(fields.Kind, validation.OneOf(Business)), validation.Rules(fields.Company, validation.Required(validation.LengthBetween[string](2, 100)))),
	)
}

func PasswordStrength() validation.Rule[password.Plaintext] {
	return validation.PasswordValue[password.Plaintext](validation.DefaultPasswordOptions())
}
