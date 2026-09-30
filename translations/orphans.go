package translations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Cursor = extensionmaintenance.Cursor
type Orphan struct {
	Key    string        `json:"key"`
	Field  Name          `json:"field"`
	Locale i18n.LocaleID `json:"locale"`
}
type OrphanPage struct {
	Orphans []Orphan
	Scanned int
	Next    Cursor
}

func InspectOrphans(ctx context.Context, m *Manager, owner extensions.OwnerName, cursor Cursor, limit int) (OrphanPage, error) {
	if err := m.Validate(); err != nil {
		return OrphanPage{}, err
	}
	page, err := extensionmaintenance.Inspect(ctx, m.store, owner, cursor, limit, orphanTable())
	if err != nil {
		return OrphanPage{}, err
	}
	result := OrphanPage{Scanned: page.Scanned, Next: page.Next}
	for _, row := range page.Rows {
		result.Orphans = append(result.Orphans, Orphan{Key: row.Key, Field: Name(row.Field), Locale: i18n.LocaleID(row.Locale)})
	}
	return result, nil
}
func PruneOrphans(ctx context.Context, m *Manager, owner extensions.OwnerName, keys []string) (int, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	return extensionmaintenance.Prune(ctx, m.store, owner, keys, orphanTable())
}
func orphanTable() extensionmaintenance.Table[store.TranslationIndex] {
	return extensionmaintenance.Table[store.TranslationIndex]{
		Name: "translations",
		Scan: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope, after string, limit int) ([]store.TranslationIndex, error) {
			f := store.TranslationFields()
			q := store.QueryFoundryModelTranslations().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope)).OrderBy(f.Key.Asc()).Limit(limit)
			if after != "" {
				q = q.Where(f.Key.Gt(after))
			}
			return store.TranslationsIndex(q).All(ctx, tx)
		},
		Lock: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope string, keys []string) ([]store.TranslationIndex, error) {
			f := store.TranslationFields()
			return store.TranslationsIndex(store.QueryFoundryModelTranslations().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope), f.Key.In(keys...)).OrderBy(f.Key.Asc())).ForUpdate().All(ctx, tx)
		},
		Identity: func(registry *extensions.Registry, owner extensions.OwnerName, scope string, row store.TranslationIndex) (model.Identity, error) {
			if !identifier.Semantic(row.Field) || i18n.LocaleID(row.Locale).Validate() != nil {
				return model.Identity{}, invalid()
			}
			return extensionrow.Restore(registry, owner, scope, extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, row.Field, row.Locale)
		},
		Key: func(row store.TranslationIndex) string { return row.Key }, SubjectKey: func(row store.TranslationIndex) string { return row.SubjectKey },
		Delete: func(ctx context.Context, tx *database.Tx, row store.TranslationIndex) error {
			_, err := store.QueryFoundryModelTranslations().Delete(ctx, tx, row.Key)
			return err
		},
	}
}

// Undeclared lists stored names that no current registration declares.
type Undeclared = extensionmaintenance.Undeclared

// InspectUndeclared lists stored field names in owner's current scope that no
// field registered with m declares, for example after renaming a slot field
// without pinning its stored name. It reads names only and modifies nothing.
func InspectUndeclared(ctx context.Context, m *Manager, owner extensions.OwnerName) (Undeclared, error) {
	if err := m.Validate(); err != nil {
		return Undeclared{}, err
	}
	return extensionmaintenance.InspectUndeclared(ctx, m.store, owner, func(ctx context.Context, tx *database.Tx, scope string, limit int) ([]string, error) {
		f := store.TranslationFields()
		return query.SelectValue(store.QueryFoundryModelTranslations().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope)), f.Field.Value()).Distinct().OrderBy(f.Field.Asc()).Limit(limit).All(ctx, tx)
	}, func(scope, name string) bool {
		_, declared := m.fields[extensions.Digest(scope, name)]
		return declared
	})
}
