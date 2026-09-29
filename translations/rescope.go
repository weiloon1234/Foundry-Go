package translations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// StaleRow is one translation recorded under an earlier scope of its owner and
// its key in the owner's current scope. Conflicts in a RescopePage are stale
// rows whose current-scope equivalent already exists.
type StaleRow = extensionmaintenance.Stale
type RescopePage = extensionmaintenance.RescopePage

// InspectStale lists, without writing, translations whose owner column names
// this registered owner but whose scope is not its current scope, typically
// after an owner table was renamed without extensions.OwnerOptions.StorageModel;
// declare the old name in OwnerOptions.PreviousModels to adopt them.
func InspectStale(ctx context.Context, m *Manager, owner extensions.OwnerName, cursor Cursor, limit int) (RescopePage, error) {
	if err := m.Validate(); err != nil {
		return RescopePage{}, err
	}
	return extensionmaintenance.Rescope(ctx, m.store, owner, cursor, limit, false, rescopeTable())
}

// Rescope moves one bounded page of stale translations into the owner's current
// scope, preserving field, locale, text and creation time. Rows are verified and
// locked first; an existing current-scope translation is never overwritten.
func Rescope(ctx context.Context, m *Manager, owner extensions.OwnerName, cursor Cursor, limit int) (RescopePage, error) {
	if err := m.Validate(); err != nil {
		return RescopePage{}, err
	}
	return extensionmaintenance.Rescope(ctx, m.store, owner, cursor, limit, true, rescopeTable())
}
func rescopeTable() extensionmaintenance.RescopeTable[store.TranslationIndex] {
	return extensionmaintenance.RescopeTable[store.TranslationIndex]{
		Name: "translations.rescope",
		Scan: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, current, after string, limit int, lock bool) ([]store.TranslationIndex, error) {
			f := store.TranslationFields()
			q := store.QueryFoundryModelTranslations().Where(f.Owner.Eq(string(owner)), f.Scope.Ne(current)).OrderBy(f.Key.Asc()).Limit(limit)
			if after != "" {
				q = q.Where(f.Key.Gt(after))
			}
			if lock {
				return store.TranslationsIndex(q).ForUpdate().All(ctx, tx)
			}
			return store.TranslationsIndex(q).All(ctx, tx)
		},
		Row: func(row store.TranslationIndex) (extensionrow.Identity, []string) {
			return extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, []string{row.Field, row.Locale}
		},
		Exists: func(ctx context.Context, tx *database.Tx, key string) (bool, error) {
			f := store.TranslationFields()
			return store.QueryFoundryModelTranslations().Where(f.Key.Eq(key)).Exists(ctx, tx)
		},
		Move: func(ctx context.Context, tx *database.Tx, row store.TranslationIndex, subject extensions.Subject, key string) error {
			if !identifier.Semantic(row.Field) || i18n.LocaleID(row.Locale).Validate() != nil {
				return invalid()
			}
			q := store.QueryFoundryModelTranslations()
			stored, err := q.RequireFind(ctx, tx, row.Key)
			if err != nil {
				return err
			}
			if _, err := q.Delete(ctx, tx, row.Key); err != nil {
				return err
			}
			_, err = q.Create(ctx, tx, store.TranslationDraft{}.SetKey(key).SetOwner(row.Owner).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetIdentity(subject.Identity).SetField(stored.Field).SetLocale(stored.Locale).SetValue(stored.Value).SetCreatedAt(stored.CreatedAt).SetUpdatedAt(stored.UpdatedAt))
			return err
		},
	}
}
