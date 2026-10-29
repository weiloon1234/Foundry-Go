package profiles_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func TestPublicModelExtensionConsumerAndORMLifecycle(t *testing.T) {
	config := pgtest.Config(t)
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Prepare(adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	// Construction captures borrowed services. The factory is invoked only after
	// assembly, for each ORM write; it never discovers services globally.
	var services profiles.Services
	declaration, err := orm.NewObserver[profiles.Profile]("profile.extensions").Declare(func() orm.Hooks[profiles.Profile] { return services.CleanupHooks() })
	if err != nil {
		t.Fatal(err)
	}
	observers, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindObservers(observers); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	schema := pgtest.Namespace(t, db)
	owners, err := extensions.NewRegistry(profiles.Owners.Registration())
	if err != nil {
		t.Fatal(err)
	}
	storeConfig := extensions.DefaultConfig()
	storeConfig.Schema = schema
	store, err := extensions.New(db, owners, storeConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE profiles(id uuid PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`); err != nil {
			return err
		}
		for _, definitions := range [][]migrate.Definition{attachments.Migrations(), metadata.Migrations(), translations.Migrations(), settings.Migrations(), countries.Migrations()} {
			for _, definition := range definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(ctx, sql); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := profiles.Files.Bind(backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	disks, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	services, err = profiles.New(store, disks, engine, locales)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := services.Attachments.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	var profile profiles.Profile
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		var err error
		profile, err = orm.Insert(ctx, tx, profiles.Profiles.Table, orm.TextSet(profiles.Profiles.Name, "Consumer"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	preferences := profiles.Preferences{Dark: true, Tags: []string{"saved"}}
	if err := services.SavePreferences(t.Context(), profile, preferences); err != nil {
		t.Fatal(err)
	}
	preferences.Tags[0] = "caller changed"
	stored, err := profiles.PreferencesKey.Get(t.Context(), services.Metadata, profiles.ProfileIdentity.ReferenceOf(profile))
	snapshot, ok := stored.Get()
	if err != nil || !ok || !snapshot.Dark || snapshot.Tags[0] != "saved" {
		t.Fatal("typed DTO metadata", err)
	}
	if err := services.SaveLabel(t.Context(), profile, "en", "Profile"); err != nil {
		t.Fatal(err)
	}
	label, err := profiles.Label.Resolve(t.Context(), services.Translations, profiles.ProfileIdentity.ReferenceOf(profile), "ms")
	resolved, ok := label.Get()
	if err != nil || !ok || resolved.Locale != "en" {
		t.Fatal("typed locale fallback", err)
	}
	if size, err := services.DefaultPageSize(t.Context()); err != nil || size != 20 {
		t.Fatal("setting default", err)
	}
	if err := profiles.PageSize.Ensure(t.Context(), services.Settings, 40); err != nil {
		t.Fatal(err)
	}
	if size, err := services.DefaultPageSize(t.Context()); err != nil || size != 40 {
		t.Fatal("cached setting was not invalidated by its write", err)
	}
	if report, err := settings.Reconcile(t.Context(), services.Settings); err != nil || len(report.Incompatible)+len(report.Upgraded)+len(report.Presented) != 0 {
		t.Fatal("startup settings reconciliation", report, err)
	}
	if result, err := countries.Seed(t.Context(), store); err != nil || result.Rows != countries.BuiltinCount {
		t.Fatal("explicit country seed", err)
	}
	if result, err := countries.Find(t.Context(), store, "MY"); err != nil || !result.IsSet() {
		t.Fatal("typed country query", err)
	}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 40, 40))); err != nil {
		t.Fatal(err)
	}
	upload, err := services.ReplaceAvatar(t.Context(), profile, attachments.Upload{Source: bytes.NewReader(imageBytes.Bytes()), OriginalName: "avatar.png"})
	file, ok := upload.Attachment.Get()
	if err != nil || !ok || file.Info().Width != 32 {
		t.Fatal("declarative image attachment", err)
	}
	if _, err := profiles.Localized.ForLocale("en").Add(t.Context(), services.Attachments, profiles.ProfileIdentity.ReferenceOf(profile), attachments.Upload{Source: strings.NewReader("English file")}); err != nil {
		t.Fatal(err)
	}
	batch, err := services.LoadLocalized(t.Context(), []model.Reference[profiles.Profile, model.ID[profiles.Profile]]{profiles.ProfileIdentity.ReferenceOf(profile)})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := batch.Get(profiles.ProfileIdentity.ReferenceOf(profile))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := loaded.Resolve("ms")
	selected, ok := fallback.Get()
	if err != nil || !ok || selected.Locale() != "en" {
		t.Fatal("localized file batch", err)
	}
	// Matching goes through the same bounded extension reads as the legacy API.
	// The owner query still owns visibility and caller filters.
	attachmentMatch, err := attachments.Matching(t.Context(), services.Attachments, profiles.Localized.ForLocale("en"))
	if err != nil {
		t.Fatal(err)
	}
	metadataMatch, err := metadata.Matching(t.Context(), services.Metadata, profiles.PreferencesKey, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	translationMatch, err := translations.Matching(t.Context(), services.Translations, profiles.Label, "en", "Profile")
	if err != nil {
		t.Fatal(err)
	}
	matches := []orm.Predicate[profiles.Profile]{attachmentMatch, metadataMatch, translationMatch}
	for _, predicate := range matches {
		if err := store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			count, err := orm.Count(ctx, tx, profiles.Profiles.Query().Where(predicate))
			if err != nil || count != 1 {
				t.Fatal("extension match", count, err)
			}
			count, err = orm.Count(ctx, tx, profiles.Profiles.Query().Where(predicate, orm.TextEq(profiles.Profiles.Name, "not this profile")))
			if err != nil || count != 0 {
				t.Fatal("matching lost caller filter", count, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Delete(ctx, tx, profiles.Profiles.Table.Query(), profiles.Profiles.Table.Key(), profile.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, predicate := range matches {
		if err := store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			count, err := orm.Count(ctx, tx, profiles.Profiles.Query().Where(predicate))
			if err != nil || count != 0 {
				t.Fatal("matching bypassed current soft-delete visibility", count, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := profiles.Avatar.List(t.Context(), services.Attachments, profiles.ProfileIdentity.ReferenceOf(profile)); !errors.Is(err, database.NotFound) {
		t.Fatal("ORM soft delete not respected", err)
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Restore(ctx, tx, profiles.Profiles.Table, profile.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Avatar.Find(t.Context(), services.Attachments, profiles.ProfileIdentity.ReferenceOf(profile), file.ID()); err != nil {
		t.Fatal("restored attachment lost", err)
	}
	rollback := errors.New("rollback deletion")
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := orm.ForceDelete(ctx, tx, profiles.Profiles.Table.Query(), profiles.Profiles.Soft, profiles.Profiles.Table.Key(), profile.ID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := profiles.Avatar.Find(t.Context(), services.Attachments, profiles.ProfileIdentity.ReferenceOf(profile), file.ID()); err != nil {
		t.Fatal("parent rollback lost attachment", err)
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.ForceDelete(ctx, tx, profiles.Profiles.Table.Query(), profiles.Profiles.Soft, profiles.Profiles.Table.Key(), profile.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if state, err := services.Attachments.Inspect(t.Context(), upload.Operation); err != nil || state.State != attachments.Cleaned {
		t.Fatal("ORM hard-delete observer missed files", err)
	}
	if page, err := metadata.InspectOrphans(t.Context(), services.Metadata, profiles.Owners.Name(), metadata.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("metadata lifecycle rows retained", err)
	}
	if page, err := translations.InspectOrphans(t.Context(), services.Translations, profiles.Owners.Name(), translations.Cursor{}, 10); err != nil || len(page.Orphans) != 0 {
		t.Fatal("translation lifecycle rows retained", err)
	}
}
