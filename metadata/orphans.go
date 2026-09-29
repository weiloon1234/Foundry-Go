package metadata

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Cursor resumes a bounded scan for this feature and registered owner. It is
// not a capability or a stable snapshot across separate pages.
type Cursor = extensionmaintenance.Cursor
type Orphan struct {
	Key  string `json:"key"`
	Name Name   `json:"name"`
}
type OrphanPage struct {
	Orphans []Orphan
	Scanned int
	Next    Cursor
}

// InspectOrphans never deletes. The limit bounds scanned rows, not matches, so
// sparse orphan sets cannot repeatedly starve later rows.
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
		result.Orphans = append(result.Orphans, Orphan{Key: row.Key, Name: Name(row.Name)})
	}
	return result, nil
}

// PruneOrphans locks exact inspected row keys and rechecks current owners,
// including soft-deleted models. Existing and foreign rows remain untouched.
func PruneOrphans(ctx context.Context, m *Manager, owner extensions.OwnerName, keys []string) (int, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	return extensionmaintenance.Prune(ctx, m.store, owner, keys, orphanTable())
}
func rowIdentity(row store.Meta) extensionrow.Identity {
	return extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}
}
func maintenanceIdentity(registry *extensions.Registry, owner extensions.OwnerName, scope string, row store.MetaIndex) (model.Identity, error) {
	if !identifier.Semantic(row.Name) || row.Version == 0 {
		return model.Identity{}, invalid()
	}
	return extensionrow.Restore(registry, owner, scope, extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, row.Name)
}
func orphanTable() extensionmaintenance.Table[store.MetaIndex] {
	return extensionmaintenance.Table[store.MetaIndex]{
		Name: "metadata",
		Scan: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope, after string, limit int) ([]store.MetaIndex, error) {
			f := store.MetaFields()
			q := store.QueryFoundryModelMetadata().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope)).OrderBy(f.Key.Asc()).Limit(limit)
			if after != "" {
				q = q.Where(f.Key.Gt(after))
			}
			return store.MetadataIndex(q).All(ctx, tx)
		},
		Lock: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope string, keys []string) ([]store.MetaIndex, error) {
			f := store.MetaFields()
			return store.MetadataIndex(store.QueryFoundryModelMetadata().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope), f.Key.In(keys...)).OrderBy(f.Key.Asc())).ForUpdate().All(ctx, tx)
		},
		Identity: maintenanceIdentity,
		Key:      func(row store.MetaIndex) string { return row.Key }, SubjectKey: func(row store.MetaIndex) string { return row.SubjectKey },
		Delete: func(ctx context.Context, tx *database.Tx, row store.MetaIndex) error {
			_, err := store.QueryFoundryModelMetadata().Delete(ctx, tx, row.Key)
			return err
		},
	}
}

// StaleRow is one metadata row recorded under an earlier scope of its owner and
// its key in the owner's current scope. RescopePage lists rows inspected or
// moved; Conflicts are stale rows whose current-scope key already exists.
type StaleRow = extensionmaintenance.Stale
type RescopePage = extensionmaintenance.RescopePage

// InspectStale lists, without writing, rows whose owner column names this
// registered owner but whose scope is not the owner's current scope. They are
// invisible to ordinary reads, typically after an owner table was renamed
// without declaring extensions.OwnerOptions.StorageModel; declare the old name
// in OwnerOptions.PreviousModels to adopt them. Conflicts, Missing subjects and
// Undeclared models are reported and left in place.
func InspectStale(ctx context.Context, m *Manager, owner extensions.OwnerName, cursor Cursor, limit int) (RescopePage, error) {
	if err := m.Validate(); err != nil {
		return RescopePage{}, err
	}
	return extensionmaintenance.Rescope(ctx, m.store, owner, cursor, limit, false, rescopeTable())
}

// Rescope moves one bounded page of stale rows into the owner's current scope,
// preserving name, version, value and creation time. Each row is verified and
// locked first; a conflicting current-scope row is never overwritten.
func Rescope(ctx context.Context, m *Manager, owner extensions.OwnerName, cursor Cursor, limit int) (RescopePage, error) {
	if err := m.Validate(); err != nil {
		return RescopePage{}, err
	}
	return extensionmaintenance.Rescope(ctx, m.store, owner, cursor, limit, true, rescopeTable())
}
func rescopeTable() extensionmaintenance.RescopeTable[store.MetaIndex] {
	return extensionmaintenance.RescopeTable[store.MetaIndex]{
		Name: "metadata.rescope",
		Scan: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, current, after string, limit int, lock bool) ([]store.MetaIndex, error) {
			f := store.MetaFields()
			q := store.QueryFoundryModelMetadata().Where(f.Owner.Eq(string(owner)), f.Scope.Ne(current)).OrderBy(f.Key.Asc()).Limit(limit)
			if after != "" {
				q = q.Where(f.Key.Gt(after))
			}
			if lock {
				return store.MetadataIndex(q).ForUpdate().All(ctx, tx)
			}
			return store.MetadataIndex(q).All(ctx, tx)
		},
		Row: func(row store.MetaIndex) (extensionrow.Identity, []string) {
			return extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, []string{row.Name}
		},
		Exists: func(ctx context.Context, tx *database.Tx, key string) (bool, error) {
			f := store.MetaFields()
			return store.QueryFoundryModelMetadata().Where(f.Key.Eq(key)).Exists(ctx, tx)
		},
		Move: func(ctx context.Context, tx *database.Tx, row store.MetaIndex, subject extensions.Subject, key string) error {
			if !identifier.Semantic(row.Name) || row.Version == 0 {
				return invalid()
			}
			q := store.QueryFoundryModelMetadata()
			stored, err := q.RequireFind(ctx, tx, row.Key)
			if err != nil {
				return err
			}
			if _, err := q.Delete(ctx, tx, row.Key); err != nil {
				return err
			}
			_, err = q.Create(ctx, tx, store.MetaDraft{}.SetKey(key).SetOwner(row.Owner).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetIdentity(subject.Identity).SetName(stored.Name).SetVersion(stored.Version).SetValue(stored.Value).SetCreatedAt(stored.CreatedAt).SetUpdatedAt(stored.UpdatedAt))
			return err
		},
	}
}
