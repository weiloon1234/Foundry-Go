package translations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
			f := store.Translations
			q := store.Translations.Query().Where(f.Owner.Eq(string(owner)), f.Scope.Ne(current)).OrderBy(f.KeyColumn.Asc()).Limit(limit)
			if after != "" {
				q = q.Where(f.KeyColumn.Gt(after))
			}
			if lock {
				return rows.ForUpdate(rows.Read(store.TranslationsIndex(q))).All(ctx, tx)
			}
			return rows.Read(store.TranslationsIndex(q)).All(ctx, tx)
		},
		Row: func(row store.TranslationIndex) (extensionrow.Identity, []string) {
			return extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, []string{row.Field, row.Locale}
		},
		Exists: func(ctx context.Context, tx *database.Tx, key string) (bool, error) {
			f := store.Translations
			return store.Translations.Query().Where(f.KeyColumn.Eq(key)).Exists(ctx, tx)
		},
		Move: func(ctx context.Context, tx *database.Tx, row store.TranslationIndex, subject extensions.Subject, key string) error {
			if !identifier.Semantic(row.Field) || i18n.LocaleID(row.Locale).Validate() != nil {
				return invalid()
			}
			q := store.Translations.Query()
			stored, err := q.Where(store.Translations.Key().Eq(row.Key)).Require(ctx, tx)
			if err != nil {
				return err
			}
			if _, err := orm.Delete(ctx, tx, q, store.Translations.Key(), row.Key); err != nil {
				return err
			}
			_, err = store.Translations.Insert(ctx, tx, store.Translations.KeyColumn.Set(key), store.Translations.Owner.Set(row.Owner), store.Translations.Scope.Set(subject.Scope), store.Translations.SubjectKey.Set(subject.Key), store.Translations.Identity.Set(subject.Identity), store.Translations.Field.Set(stored.Field), store.Translations.Locale.Set(stored.Locale), store.Translations.Value.Set(stored.Value), store.Translations.CreatedAt.Set(stored.CreatedAt), store.Translations.UpdatedAt.Set(stored.UpdatedAt))
			return err
		},
	}
}
