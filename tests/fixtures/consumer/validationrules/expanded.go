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
		fields.Email.Rules(validation.Bail(validation.Email[string](), validation.MaxLength[string](254), validation.Unique(addresses))),
		fields.Tags.Rules(validation.Bail(validation.ItemsBetween[[]string](1, 8), validation.Distinct[[]string](), validation.Each[[]string](validation.AlphaDash[string]()))),
		validation.When(fields.Kind.Rules(validation.OneOf(Business)), fields.Company.Rules(validation.Required(validation.LengthBetween[string](2, 100)))),
	)
}

func PasswordStrength() validation.Rule[password.Plaintext] {
	return validation.PasswordValue[password.Plaintext](validation.DefaultPasswordOptions())
}
