package reporting_test

import (
	"context"
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"io"
	"log/slog"
	"testing"
)

func TestConfiguredReportManagerUsesDeclaredTypedSource(t *testing.T) {
	fixture := openFixture(t)
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	db := infrastructure.DefaultConnectionSettings()
	db.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": db}
	s.Features.Reports.Enabled = true
	s.Features.Reports.Config.Schema = fixture.schema
	s.Features.Locales.Enabled = true
	app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Features(func(application.Services) (application.FeatureDeclarations, error) {
		declarations := application.FeatureDeclarations{Reports: []datatable.Registration{reporting.Members.Registration()}, Catalog: map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"en": {}}}
		for _, key := range []i18n.MessageKey{reporting.MemberIDLabel, reporting.MemberNameLabel, reporting.NicknameLabel, reporting.StateLabel, reporting.BalanceLabel, reporting.OrderLabel} {
			declarations.Messages = append(declarations.Messages, i18n.MessageDefinition{Key: key})
			declarations.Catalog["en"][key] = i18n.Template{Text: string(key)}
		}
		return declarations, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	manager, err := app.Resources().Reports()
	if err != nil {
		t.Fatal(err)
	}
	scope := fixture.scope(t)
	page, err := reporting.ListMembers(scope.Context(), manager, fixture.guard, datatable.Request{Page: 1, Size: 1, Search: "aDA", Filters: []datatable.Filter{{Op: datatable.Equal, Column: "state", Values: []string{"active"}}}})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Name != "Ada" {
		t.Fatal("configured report lost typed source or tenant policy", err)
	}
}
