package configured

import (
	"context"
	"errors"
	"io/fs"

	"foundry.test/consumer/localization"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin configured-catalog
func CatalogApplication(ctx context.Context, deployment application.Settings, files fs.FS, messages []i18n.MessageDefinition) (*application.App, error) {
	selection := deployment.Features.Locales
	locales, err := i18n.NewLocaleSet(selection.Default, selection.Locales...)
	if err != nil {
		return nil, err
	}
	templates, err := i18n.ReadTemplates(ctx, files, locales)
	if err != nil {
		return nil, err
	}
	return application.New(deployment).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Messages: messages, Catalog: templates}, nil
	}).Build(ctx)
}

// doc-example:end configured-catalog

// doc-example:begin load-message-catalog
func LoadMessages(ctx context.Context, files fs.FS, locales i18n.LocaleCatalog) (*i18n.Catalog, error) {
	return message.Load(ctx, files, locales, i18n.CatalogOptions{}, localization.CartArgsMessage().Registration())
}

// doc-example:end load-message-catalog

// doc-example:begin format-cart-message
func FormatCart(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, args localization.CartArgs) (i18n.Result, error) {
	return localization.CartArgsMessage().Format(ctx, catalog, locale, args)
}

// doc-example:end format-cart-message

// doc-example:begin request-message
func MessageCatalog(services application.Services) (*i18n.Catalog, error) {
	return services.Locales()
}

func WelcomeRequest(ctx context.Context, catalog *i18n.Catalog, name string) (i18n.Result, error) {
	locale, ok := i18n.RequestLocale(ctx)
	if !ok {
		return i18n.Result{}, errors.New("request locale is not selected")
	}
	return localization.WelcomeArgsMessage().Format(ctx, catalog, locale, localization.WelcomeArgs{Name: name})
}

// doc-example:end request-message

// doc-example:begin localize-validation
func CheckName(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, name string) error {
	err := validation.Check(localization.NameRule(), ctx, localization.WelcomeArgs{Name: name}, validation.DefaultLimits())
	var rejected *validation.Errors
	if !errors.As(err, &rejected) {
		return err
	}
	localized, err := rejected.Localize(ctx, catalog, locale)
	if err != nil {
		return err
	}
	return localized
}

// doc-example:end localize-validation
