package validation

import (
	"github.com/weiloon1234/Foundry-Go/secret"
	"unicode"
	"unicode/utf8"
)

// PasswordOptions counts Unicode code points. Composition requirements are
// opt-in; no normalization, trimming, hashing or external breach request occurs.
type PasswordOptions struct {
	MinLength int
	MaxLength int
	Letters   bool
	MixedCase bool
	Numbers   bool
	Symbols   bool
}

func DefaultPasswordOptions() PasswordOptions { return PasswordOptions{MinLength: 12, MaxLength: 128} }
func (o PasswordOptions) Validate() error {
	if o.MinLength < 1 || o.MaxLength < o.MinLength {
		return invalid("invalid password length bounds")
	}
	return nil
}
func Password[S ~string](options PasswordOptions) Rule[S] {
	return passwordRule(options, func(v S) string { return string(v) })
}

// PasswordValue accepts redacted password input such as auth/password.Plaintext.
// The secret is inspected only during Check and never enters public metadata.
func PasswordValue[P interface{ Secret() secret.String }](options PasswordOptions) Rule[P] {
	return passwordRule(options, func(v P) string { return v.Secret().Reveal() })
}
func passwordRule[T any](options PasswordOptions, text func(T) string) Rule[T] {
	if err := options.Validate(); err != nil {
		return failed[T](err)
	}
	spec := Spec{ID: "foundry.password", Parameters: []Parameter{
		parameter("min", options.MinLength), parameter("max", options.MaxLength), parameter("letters", options.Letters), parameter("mixed_case", options.MixedCase), parameter("numbers", options.Numbers), parameter("symbols", options.Symbols),
	}}
	return valueRule(spec, true, func(s *execution, input T) (bool, error) {
		v := text(input)
		if !textValue(s, v) {
			return false, nil
		}
		count := utf8.RuneCountInString(v)
		if count < options.MinLength || count > options.MaxLength {
			return false, nil
		}
		var letter, upper, lower, number, symbol bool
		for _, r := range v {
			letter = letter || unicode.IsLetter(r)
			upper = upper || unicode.IsUpper(r)
			lower = lower || unicode.IsLower(r)
			number = number || unicode.IsNumber(r)
			symbol = symbol || unicode.IsPunct(r) || unicode.IsSymbol(r)
		}
		return (!options.Letters || letter) && (!options.MixedCase || upper && lower) && (!options.Numbers || number) && (!options.Symbols || symbol), nil
	})
}
