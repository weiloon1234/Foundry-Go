package configured

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/settings"
)

// doc-example:begin declare-runtime-setting
var SiteName = settings.Define("site.name", 1, contract.StringJSON[string](),
	settings.Presentation{Label: "Site name", Group: "site", Public: true})

func SettingsApplication(ctx context.Context, deployment application.Settings) (*application.App, error) {
	deployment.Features.Extensions.Enabled = true
	return application.New(deployment).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Settings: []settings.Registration{SiteName.Registration(), MailPortRegistration()}}, nil
	}).Build(ctx)
}

// doc-example:end declare-runtime-setting

// doc-example:begin seed-runtime-setting
func SeedSiteName(ctx context.Context, tx *database.Tx, manager *settings.Manager) error {
	return SiteName.EnsureIn(ctx, tx, manager, "My site")
}

func ReadSiteName(ctx context.Context, manager *settings.Manager) (string, error) {
	return SiteName.GetOr(ctx, manager, "Unnamed site")
}

func SaveSiteName(ctx context.Context, manager *settings.Manager, name string) error {
	return SiteName.Upsert(ctx, manager, name)
}

func SaveSiteNameIn(ctx context.Context, tx *database.Tx, manager *settings.Manager, name string) error {
	return SiteName.UpsertIn(ctx, tx, manager, name)
}

// doc-example:end seed-runtime-setting

// doc-example:begin upgrade-runtime-setting
var MailPort = settings.Define("mail.port", 2, contract.IntegerJSON[int64](),
	settings.Presentation{Kind: settings.Number, Group: "mail"})

func MailPortRegistration() settings.Registration {
	return MailPort.RegistrationWith(settings.Options[int64]{Upgrades: []settings.Upgrade[int64]{
		settings.UpgradeFrom(1, contract.StringJSON[string](), func(_ context.Context, old string) (int64, error) {
			return strconv.ParseInt(old, 10, 64)
		}),
	}})
}

func ReconcileSettings(ctx context.Context, manager *settings.Manager) error {
	_, err := settings.Reconcile(ctx, manager)
	return err
}

// doc-example:end upgrade-runtime-setting
