package validationrules

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin bind-validation-rule
func bindRegistrationCheck(rule validation.Rule[Registration]) func(context.Context, Registration, validation.Limits) error {
	bound := rule
	check := func(ctx context.Context, input Registration, limits validation.Limits) error {
		return validation.Check(bound, ctx, input, limits)
	}
	return check
}

// doc-example:end bind-validation-rule

// Username composes published bounds with an application-owned format rule.
// doc-example:begin publish-username-bounds
func Username(spec validation.Spec, check func(context.Context, string) (bool, error)) validation.Rule[string] {
	format := validation.WithTranslation(validation.Custom(spec, check), UsernameArgsMessage(), UsernameArgs{})
	return validation.Bail(validation.NonBlank[string](), validation.LengthBetween[string](5, 24), format)
}

// doc-example:end publish-username-bounds

// doc-example:begin relative-time-rules
func relativeTimeRules(services application.Services) (validation.Rule[temporal.DateTime], validation.Rule[temporal.Date]) {
	future := validation.AfterNow[temporal.DateTime](services.Time())
	dueDate := validation.AfterOrEqualToday[temporal.Date](services.Time())
	return future, dueDate
}

// doc-example:end relative-time-rules
