// Package localization exercises generated message arguments and shared feature
// labels from an independent module using only public framework imports.
package localization

import (
	"context"
	"embed"
	"io/fs"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

//foundry:message key=welcome
type WelcomeArgs struct {
	Name string `json:"name"`
}

// doc-example:begin typed-plural-message

//foundry:message key=cart.items plural=count
type CartArgs struct {
	Name  string          `json:"name"`
	Count decimal.Decimal `json:"count"`
}

// doc-example:end typed-plural-message

//foundry:message key=position plural=position kind=ordinal
type PositionArgs struct {
	Position int64 `json:"position,string"`
}

//foundry:message key=wire.literals
type LiteralArgs struct {
	Text    string `json:"text,string"`
	Enabled bool   `json:"enabled,string"`
}

//foundry:message key=fields.name
type NameLabel struct{}

//foundry:message key=permissions.manage
type ManageLabel struct{}

// doc-example:begin enum-labels

//foundry:enum labels=enum.localization.status
type Status string

const (
	Draft Status = "draft"
	Ready Status = "ready"
)

// doc-example:end enum-labels

//go:embed locales
var catalogs embed.FS

//go:embed frontend
var frontendCatalogs embed.FS

// FrontendFS exposes the embedded frontend translation directory.
func FrontendFS() (fs.FS, error) {
	return fs.Sub(frontendCatalogs, "frontend")
}

func Catalog(ctx context.Context) (*i18n.Catalog, error) {
	definitions, err := message.Definitions(WelcomeArgsMessage().Registration(), CartArgsMessage().Registration(), PositionArgsMessage().Registration(), LiteralArgsMessage().Registration(), NameLabelMessage().Registration(), ManageLabelMessage().Registration())
	if err != nil {
		return nil, err
	}
	labels, err := Draft.EnumDescriptor().LabelDefinitions()
	if err != nil {
		return nil, err
	}
	definitions = append(definitions, labels...)
	locales, err := i18n.NewLocaleSet("en", "en", "ms", "ar")
	if err != nil {
		return nil, err
	}
	source, err := fs.Sub(catalogs, "locales")
	if err != nil {
		return nil, err
	}
	return i18n.Load(ctx, source, locales, i18n.CatalogOptions{}, definitions...)
}

func Welcome(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, name string) (i18n.Result, error) {
	return WelcomeArgsMessage().Format(ctx, catalog, locale, WelcomeArgs{Name: name})
}

// doc-example:begin rule-message-arguments

// RuleArgs is approved declaration data, never submitted field contents.
//
//foundry:message key=profile.validation.name
type RuleArgs struct {
	Attribute string `json:"attribute"`
	Team      string `json:"team"`
}

// doc-example:end rule-message-arguments

// PreparedWelcome captures public arguments once for later CLI or job output.
func PreparedWelcome(ctx context.Context, name string) (i18n.PreparedMessage, error) {
	return WelcomeArgsMessage().Bind(ctx, WelcomeArgs{Name: name}, i18n.Template{Text: "Welcome {{name}}."})
}
