package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	profilemodel "foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/settings"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfiguredPersistentActorsAndModelFeatures(t *testing.T) {
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Image.Enabled = true
	schema := "configured_" + strings.ToLower(rand.Text())
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection, "other": connection}
	disk := infrastructure.DefaultDiskSettings()
	disk.Local.Root = t.TempDir()
	s.Services.Storage.Default = profilemodel.Files.ID()
	s.Services.Storage.Disks = infrastructure.Disks{profilemodel.Files.ID(): disk}
	s.Features.Auth.Sessions.Enabled = true
	s.Features.Auth.Sessions.Schema = schema
	s.Features.Auth.Tokens.Enabled = true
	s.Features.Auth.Tokens.Schema = schema
	browserPolicy := application.DefaultBrowserGuardSettings("web")
	browserPolicy.Cookie = "configured_member"
	browserPolicy.Options.Secure = false
	s.Features.Auth.Browser.Guards = application.BrowserGuards{"web": browserPolicy}
	s.Features.Extensions.Enabled = true
	s.Features.Extensions.Schema = schema
	s.Features.Locales.Enabled = true
	s.Features.Locales.Locales = []i18n.LocaleID{"en", "ms"}
	s.Features.Attachments.Enabled = true
	s.Features.Notifications.Enabled = true
	s.Features.Notifications.Schema = schema
	s.Features.Audit.Enabled = true
	s.Features.Audit.Schema = schema
	s.Features.Reports.Enabled = true
	s.Features.Reports.Config.Schema = schema
	s.Features.Health.Enabled = true
	s.Features.Observability.Enabled = true
	app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{
			Owners:       []extensions.Declaration{profilemodel.Owners.Registration()},
			Settings:     []settings.Registration{profilemodel.PageSize.Registration()},
			Metadata:     []metadata.Registration{profilemodel.PreferencesKey.Registration()},
			Translations: []translations.Registration{profilemodel.Label.Registration()},
			Attachments:  []attachments.Registration{profilemodel.Documents.Registration()},
		}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	targets := app.Migrations()
	if len(targets) != 1 || targets[0].Schema != schema {
		t.Fatal("migration targets were not grouped")
	}
	original := targets[0].Definitions[0].SQL[0]
	targets[0].Definitions[0].SQL[0] = "mutated"
	if app.Migrations()[0].Definitions[0].SQL[0] != original {
		t.Fatal("migration snapshot was mutable")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services := app.Resources()
	db, err := services.Database()
	if err != nil {
		t.Fatal(err)
	}
	// Startup must not create the schema. Migration execution is an explicit task.
	var exists bool
	if err := database.ScanOne(t.Context(), db, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", []any{schema}, &exists); err != nil || exists {
		t.Fatal("startup applied migrations", err)
	}
	if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	if err := withinSchema(t.Context(), db, schema, func(tx *database.Tx) error {
		for _, target := range app.Migrations() {
			for _, d := range target.Definitions {
				for _, sql := range d.SQL {
					if _, err := tx.Exec(t.Context(), sql); err != nil {
						return err
					}
				}
			}
		}
		_, err := tx.Exec(t.Context(), "CREATE TABLE profiles(id uuid PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	memberID, err := model.NewID[Member]()
	if err != nil {
		t.Fatal(err)
	}
	operatorID, err := model.NewID[Operator]()
	if err != nil {
		t.Fatal(err)
	}
	member := Member{ID: memberID, Name: "Member"}
	operator := Operator{ID: operatorID, Department: "Operations"}
	mp := auth.DefineProvider("configured.members", (Member{}).FoundryReference(), func(context.Context, model.ID[Member]) (value.Optional[Member], error) { return value.Set(member), nil }, func(context.Context, Member) (bool, error) { return true, nil })
	op := auth.DefineProvider("configured.operators", (Operator{}).FoundryReference(), func(context.Context, model.ID[Operator]) (value.Optional[Operator], error) {
		return value.Set(operator), nil
	}, func(context.Context, Operator) (bool, error) { return true, nil })
	members, err := application.NewBrowserGuard(services, "", mp, "member.cookie")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := auth.NewAccessScopes[Operator]()
	if err != nil {
		t.Fatal(err)
	}
	operators, err := application.NewTokenGuard(services, "", op, "operator.bearer", allowed)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(member.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := members.Sessions.Issue(t.Context(), proof, session.IssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	operatorProof, err := auth.NewProof(operator.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	bearer, err := operators.Tokens.Issue(t.Context(), operatorProof, token.IssueOptions[Operator]{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	memberRoute := http.Authenticated(profileEndpoint("configured.member", "/member", http.POST).WithMiddleware(members.Browser.Middleware()), members.Binding).Handle(func(_ context.Context, actor Member, _ MemberProfileInput) (Profile, error) {
		return Profile{Name: actor.Name, Kind: "member"}, nil
	})
	operatorRoute := http.Authenticated(profileEndpoint("configured.operator", "/operator", http.POST), operators.Binding).Handle(func(_ context.Context, actor Operator, _ MemberProfileInput) (Profile, error) {
		return Profile{Name: actor.Department, Kind: "operator"}, nil
	})
	router, err := http.NewRouter(memberRoute, operatorRoute)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(path, cookie, bearer, origin string) int {
		request := httptest.NewRequest("POST", "http://fixture.test"+path, nil)
		if cookie != "" {
			request.AddCookie(&stdhttp.Cookie{Name: string(browserPolicy.Cookie), Value: cookie})
		}
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := serve("/member", browser.Secret().Reveal(), "", "http://fixture.test"); code != 200 {
		t.Fatalf("member status %d", code)
	}
	if code := serve("/member", browser.Secret().Reveal(), "", ""); code != 403 {
		t.Fatalf("CSRF status %d", code)
	}
	if code := serve("/operator", "", bearer.AccessSecret().Reveal(), ""); code != 200 {
		t.Fatalf("operator status %d", code)
	}
	if code := serve("/operator", "", browser.Secret().Reveal(), ""); code != 401 {
		t.Fatalf("wrong actor status %d", code)
	}
	if _, err := members.Sessions.Revoke(t.Context(), browser.Secret()); err != nil {
		t.Fatal(err)
	}
	if code := serve("/member", browser.Secret().Reveal(), "", "http://fixture.test"); code != 401 {
		t.Fatalf("revoked session status %d", code)
	}
	store, err := services.ExtensionStore()
	if err != nil {
		t.Fatal(err)
	}
	var profile profilemodel.Profile
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		var err error
		profile, err = profilemodel.QueryProfiles().Create(ctx, tx, profilemodel.ProfileDraft{}.SetName("Configured"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	preferences, err := services.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if err := profilemodel.PreferencesKey.Set(t.Context(), preferences, profile.FoundryReference(), profilemodel.Preferences{Dark: true}); err != nil {
		t.Fatal(err)
	}
	found, err := profilemodel.PreferencesKey.Get(t.Context(), preferences, profile.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	pref, ok := found.Get()
	if !ok || !pref.Dark {
		t.Fatal("metadata not persisted")
	}
	config, err := services.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if err := profilemodel.PageSize.Ensure(t.Context(), config, 20); err != nil {
		t.Fatal(err)
	}
	if err := profilemodel.PageSize.Set(t.Context(), config, 50); err != nil {
		t.Fatal(err)
	}
	page, err := profilemodel.PageSize.GetOr(t.Context(), config, 20)
	if err != nil || page != 50 {
		t.Fatal("setting not persisted", err)
	}
	localized, err := services.Translations()
	if err != nil {
		t.Fatal(err)
	}
	if err := profilemodel.Label.Set(t.Context(), localized, profile.FoundryReference(), "en", "Configured label"); err != nil {
		t.Fatal(err)
	}
	text, err := profilemodel.Label.Get(t.Context(), localized, profile.FoundryReference(), "en")
	if err != nil {
		t.Fatal(err)
	}
	label, ok := text.Get()
	if !ok || label != "Configured label" {
		t.Fatal("translation not persisted")
	}
	scope, err := services.AuditScope()
	if err != nil {
		t.Fatal(err)
	}
	action := audit.Define[Profile]("configured.action", 1)
	var id audit.ActionID[Profile]
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		return scope.Within(t.Context(), tx, func(child *database.Tx, recorder *audit.Recorder) error {
			var err error
			id, err = action.Record(t.Context(), child, recorder, Profile{Name: "recorded"})
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		return scope.Within(t.Context(), tx, func(child *database.Tx, recorder *audit.Recorder) error {
			found, err := action.Find(t.Context(), child, recorder, id)
			if err == nil && !found.IsSet() {
				return errors.New("audit row missing")
			}
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	other, _ := services.Databases.Connection("other")
	if err := other.Transaction(t.Context(), func(tx *database.Tx) error {
		return scope.Within(t.Context(), tx, func(*database.Tx, *audit.Recorder) error { return nil })
	}); err == nil {
		t.Fatal("audit accepted another pool")
	}
	health, err := services.Health()
	if err != nil {
		t.Fatal(err)
	}
	report, err := health.Check(t.Context())
	if err != nil || !report.Ready {
		t.Fatal("configured readiness failed", err)
	}
	gate, err := services.Maintenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	files, err := services.Attachments()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profilemodel.Documents.Add(t.Context(), files, profile.FoundryReference(), attachments.Upload{Source: strings.NewReader("configured attachment"), OriginalName: "proof.txt"}); err != nil {
		t.Fatal(err)
	}
	documents, err := profilemodel.Documents.List(t.Context(), files, profile.FoundryReference())
	if err != nil || len(documents) != 1 {
		t.Fatal("configured attachment not listed", err)
	}
	// Resolved managers are framework-owned; ordinary shutdown closes these too.
	if _, err := services.Notifications(); err != nil {
		t.Fatal(err)
	}
	if _, err := services.Reports(); err != nil {
		t.Fatal(err)
	}
	if _, err := services.Attachments(); err != nil {
		t.Fatal(err)
	}
}
