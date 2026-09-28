package localization

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// NameRule attaches a generated message to a concrete field-owned rule. The
// generated argument owner is checked by Go; field labels supply attribute.
func NameRule() validation.Rule[WelcomeArgs] {
	return WelcomeArgsValidationFields().Name.WithLabel("Name").WithLabelKey("fields.name").Rules(validation.WithTranslation(
		validation.NonBlank[string]().WithMessage("Enter your name."),
		RuleArgsMessage(), RuleArgs{Team: "Foundry team"},
	))
}
func ValidationCatalog(ctx context.Context) (*i18n.Catalog, error) {
	definition, err := RuleArgsMessage().Definition()
	if err != nil {
		return nil, err
	}
	label, err := NameLabelMessage().Definition()
	if err != nil {
		return nil, err
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		return nil, err
	}
	definitions := append(validation.MessageDefinitions(), definition, label)
	return i18n.NewCatalog(ctx, locales, i18n.CatalogOptions{}, definitions, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {
		RuleArgsMessage().Key():  {Text: "{{attribute}} diperlukan oleh {{team}}."},
		NameLabelMessage().Key(): {Text: "Nama"},
		"validation.unique":      {Text: "{{attribute}} sudah digunakan."},
	}})
}
