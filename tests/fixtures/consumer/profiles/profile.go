// Package profiles demonstrates model extensions through public typed APIs.
// The application declares policy and lifecycle composition; Foundry owns rows,
// image processing, storage publication and recoverable cleanup.
package profiles

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/settings"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Profile struct {
	ID   model.ID[Profile]
	Name string
	// Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through typed assignments uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
}

//foundry:dto
type Preferences struct {
	Dark bool     `json:"dark"`
	Tags []string `json:"tags"`
}

// StorageModel pins the persisted owner identity, so renaming the profiles table
// later keeps its metadata, translations and attachments.
var Owners = extensions.DefineORMOwner("profiles", ProfileIdentity, extensions.OwnerOptions{StorageModel: "profiles"})
var Files = storage.DefineDisk("profile-files")
var Avatar = attachments.Define(Owners, "avatar", attachments.Policy{Disk: Files, Cardinality: attachments.Single, Accepted: []storage.MediaType{"image/png", "image/jpeg"}, Image: value.Set(imaging.NewPlan().Fill(32, 32, false).Format(imaging.PNG))})
var Documents = attachments.Define(Owners, "documents", attachments.Policy{Disk: Files, Cardinality: attachments.Multiple, MaxFiles: 4, Accepted: []storage.MediaType{"text/plain"}})
var Localized = attachments.Define(Owners, "localized", attachments.Policy{Disk: Files, Cardinality: attachments.Single, Localized: true, Accepted: []storage.MediaType{"text/plain"}})
var PreferencesKey = metadata.Define(Owners, "preferences", 1, PreferencesJSON())
var Label = translations.Define(Owners, "label", translations.Options{MaxBytes: 1024})
var PageSize = settings.Define("profiles.page-size", 1, contract.IntegerJSON[uint32](), settings.Presentation{Kind: settings.Number, Group: "profiles", Label: "Page size"})

type Services struct {
	Attachments  *attachments.Manager
	Metadata     *metadata.Manager
	Translations *translations.Manager
	Settings     *settings.Manager
}

func New(store *extensions.Store, disks *storage.Registry, image *imaging.Engine, locales i18n.LocaleCatalog) (Services, error) {
	var s Services
	var err error
	if s.Metadata, err = metadata.New(store, PreferencesKey.Registration()); err != nil {
		return Services{}, err
	}
	if s.Translations, err = translations.New(store, locales, Label.Registration()); err != nil {
		return Services{}, err
	}
	if s.Settings, err = settings.New(store, PageSize.RegistrationWith(settings.Options[uint32]{Cache: time.Minute})); err != nil {
		return Services{}, err
	}
	s.Attachments, err = attachments.New(attachments.Dependencies{Store: store, Disks: disks, Image: image, Locales: locales}, attachments.DefaultConfig(), Avatar.Registration(), Documents.Registration(), Localized.Registration())
	return s, err
}
func (s Services) ReplaceAvatar(ctx context.Context, profile Profile, upload attachments.Upload) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Avatar.Replace(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), upload)
}
func (s Services) SavePreferences(ctx context.Context, profile Profile, input Preferences) error {
	return PreferencesKey.Set(ctx, s.Metadata, ProfileIdentity.ReferenceOf(profile), input)
}
func (s Services) SaveLabel(ctx context.Context, profile Profile, locale i18n.LocaleID, text string) error {
	return Label.Set(ctx, s.Translations, ProfileIdentity.ReferenceOf(profile), locale, text)
}
func (s Services) DefaultPageSize(ctx context.Context) (uint32, error) {
	return PageSize.GetOr(ctx, s.Settings, 20)
}

// EnsureDefaultPageSize initializes a missing default in the caller's transaction.
func (s Services) EnsureDefaultPageSize(ctx context.Context, tx *database.Tx) error {
	return PageSize.EnsureIn(ctx, tx, s.Settings, 20)
}
func (s Services) LoadLocalized(ctx context.Context, profiles []model.Reference[Profile, model.ID[Profile]]) (attachments.LocalizedBatch[Profile, model.ID[Profile]], error) {
	return Localized.LoadLocalized(ctx, s.Attachments, profiles)
}

// CleanupHooks is supplied through an ORM observer declaration.
// Example: orm.NewObserver[Profile]("profile.extensions").Declare(services.CleanupHooks).
// Deleted also runs for force-delete; operation metadata avoids duplicate hooks.
func (s Services) CleanupHooks() orm.Hooks[Profile] {
	return orm.Hooks[Profile]{Deleted: func(ctx context.Context, tx *database.Tx, changes orm.Changes[Profile]) error {
		before, ok := changes.Before().Get()
		if !ok {
			return errors.New("profile cleanup requires prior model")
		}
		operation, ok := changes.Operation().Get()
		if !ok {
			return errors.New("profile cleanup requires lifecycle operation")
		}
		if err := metadata.Cleanup(ctx, tx, s.Metadata, Owners, ProfileIdentity.ReferenceOf(before), operation); err != nil {
			return err
		}
		if err := translations.Cleanup(ctx, tx, s.Translations, Owners, ProfileIdentity.ReferenceOf(before), operation); err != nil {
			return err
		}
		return attachments.Cleanup(ctx, tx, s.Attachments, Owners, ProfileIdentity.ReferenceOf(before), operation, nil)
	}}
}
