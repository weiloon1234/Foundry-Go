// Package phone adapts the shared phone parser to typed validation rules.
// Import it as phonevalidation alongside the phone package.
package phone

import (
	"context"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/phone"
	"github.com/weiloon1234/Foundry-Go/validation"
)

const (
	// NumberRuleID identifies the default server-only phone rule in metadata.
	NumberRuleID validation.RuleID = "phone.number"
	// DiscardExtensionRuleID identifies the rule that explicitly discards extensions.
	DiscardExtensionRuleID validation.RuleID = "phone.number_discard_extension"
)

// These immutable declarations share each custom ID's definition across rule
// trees. The slot's value belongs to each Check context, never to global state.
var (
	selectedRegion = validation.NewSlot[countries.Code]()
	rejectNumber   = declaredNumber(phone.RejectExtensions)
	discardNumber  = declaredNumber(phone.DiscardExtensions)
)

// Number validates text for a fixed region without mutating it. Empty text
// rejects; compose Optional and Nullable to declare absence semantics.
// Example: phonevalidation.Number[string](countries.Code("MY"), phone.Options{}).
func Number[S ~string](region countries.Code, options phone.Options) validation.Rule[S] {
	return validation.Provide(selectedRegion, func(context.Context, S) (countries.Code, error) {
		return region, nil
	}, numberRule[S](options, region.Validate() == nil))
}

// For validates a number against the region selected by another typed field.
// Rejections retain the number field's path and label. Wrap the whole contact
// with Optional or Nullable when absence is allowed. The region is per check;
// neither input nor registered rules are changed.
// Example: phonevalidation.For(fields.Country, fields.Number, phone.Options{}).
func For[T any, S ~string](region validation.Field[T, countries.Code], number validation.Field[T, S], options phone.Options) validation.Rule[T] {
	return validation.Provide(selectedRegion, func(_ context.Context, input T) (countries.Code, error) {
		return validation.Select(region, input)
	}, validation.Rules(number, numberRule[S](options, validation.ValidateField(region) == nil)))
}

// MessageDefinitions returns owned translation signatures for the phone rules.
// Register these alongside validation.MessageDefinitions when supplying catalog
// translations. Missing translations retain the rule's English fallback.
func MessageDefinitions() []i18n.MessageDefinition {
	return []i18n.MessageDefinition{
		{Key: "validation.phone", Parameters: []i18n.Parameter{{Name: "attribute", Kind: i18n.TextParameter}}},
		{Key: "validation.phone_without_extension", Parameters: []i18n.Parameter{{Name: "attribute", Kind: i18n.TextParameter}}},
	}
}

func numberRule[S ~string](options phone.Options, valid bool) validation.Rule[S] {
	if !valid || options.Validate() != nil {
		return validation.Custom[S](validation.Spec{ID: NumberRuleID, Message: "Invalid phone rule declaration."}, nil)
	}
	rule := rejectNumber
	if options.Extensions == phone.DiscardExtensions {
		rule = discardNumber
	}
	return validation.Requires(selectedRegion, validation.Embed(rule, func(input S) string { return string(input) }))
}

func declaredNumber(policy phone.ExtensionPolicy) validation.Rule[string] {
	definition := MessageDefinitions()[0]
	id := DiscardExtensionRuleID
	text := "Enter a valid phone number for the selected country."
	if policy == phone.RejectExtensions {
		definition = MessageDefinitions()[1]
		id = NumberRuleID
		text = "Enter a valid phone number for the selected country, without an extension."
	}
	prepared, err := i18n.PrepareMessage(definition, map[string]i18n.Argument{"attribute": i18n.Text("This field")}, i18n.Template{Text: text})
	spec := validation.Spec{ID: id, Message: text, Parameters: []validation.Parameter{
		parameter("max_bytes", phone.MaxInputBytes), parameter("extensions", policy),
	}}
	if err != nil {
		return validation.Custom[string](spec, nil)
	}
	recipe := prepared.Description()
	spec.Translation = &recipe
	return validation.Custom(spec, func(ctx context.Context, input string) (bool, error) {
		region, err := selectedRegion.Value(ctx)
		if err != nil {
			return false, err
		}
		_, err = phone.Parse(region, input, phone.Options{Extensions: policy})
		return err == nil, nil
	})
}

func parameter[T any](name string, value T) validation.Parameter {
	encoded, _ := json.Marshal(value)
	return validation.Parameter{Name: name, Value: encoded}
}
