package httpdto

import (
	"context"
	"strings"

	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

// doc-example:begin patch-rules
func UpdateRules() validation.Rule[UpdateUser] {
	f := UpdateUserValidationFields()
	return validation.All(
		validation.Rules(f.Email, validation.Optional(validation.Bail(validation.NonBlank[string](), validation.Email[string]()))),
		validation.Rules(f.Nickname, validation.Optional(validation.Nullable(validation.MinLength[string](2)))),
	)
}

// doc-example:end patch-rules

// doc-example:begin check-update
func CheckUpdate(ctx context.Context, input UpdateUser) error {
	return validation.Check(UpdateRules(), ctx, input, validation.DefaultLimits())
}

// doc-example:end check-update

// doc-example:begin custom-translated-rule
func SingleLineName() validation.Rule[string] {
	rule := validation.Custom(validation.Spec{ID: "rules.single_line_name", Message: "Enter a name on one line."},
		func(_ context.Context, name string) (bool, error) {
			return !strings.ContainsAny(name, "\r\n"), nil
		})
	return validation.WithTranslation(rule, localization.RuleArgsMessage(), localization.RuleArgs{Team: "Support"})
}

// doc-example:end custom-translated-rule

// doc-example:begin decimal-precision
func CheckAmount(ctx context.Context, amount decimal.Decimal) error {
	return validation.Check(validation.DecimalMaxPlaces(2), ctx, amount, validation.DefaultLimits())
}

func WholeUnits(amount decimal.Decimal) (int64, error) {
	return amount.Int64()
}

// doc-example:end decimal-precision

// doc-example:begin typed-validation-lookup
func EmailLookup(executor database.Executor, scopedUsers orm.Query[models.User]) databasevalidation.ModelLookup[models.User, string] {
	return databasevalidation.Lookup(executor, scopedUsers, orm.TextExpr(models.Users.Email))
}

// doc-example:end typed-validation-lookup
