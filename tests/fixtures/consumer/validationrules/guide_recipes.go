package validationrules

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func bindRegistrationCheck(rule validation.Rule[Registration]) func(context.Context, Registration, validation.Limits) error {
	// doc-example:begin bind-validation-rule
	bound := rule
	check := func(ctx context.Context, input Registration, limits validation.Limits) error {
		return validation.Check(bound, ctx, input, limits)
	}
	// doc-example:end bind-validation-rule
	return check
}

// Username composes published bounds with an application-owned format rule.
// doc-example:begin publish-username-bounds
func Username(spec validation.Spec, check func(context.Context, string) (bool, error)) validation.Rule[string] {
	format := validation.WithTranslation(validation.Custom(spec, check), UsernameArgsMessage(), UsernameArgs{})
	return validation.Bail(validation.NonBlank[string](), validation.LengthBetween[string](5, 24), format)
}

// doc-example:end publish-username-bounds

func relativeTimeRules(services application.Services) (validation.Rule[temporal.DateTime], validation.Rule[temporal.Date]) {
	// doc-example:begin relative-time-rules
	future := validation.AfterNow[temporal.DateTime](services.Time())
	dueDate := validation.AfterOrEqualToday[temporal.Date](services.Time())
	// doc-example:end relative-time-rules
	return future, dueDate
}
