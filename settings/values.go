package settings

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

type writeMode uint8

const (
	createValue writeMode = iota
	ensureValue
	upsertValue
	setValue
)

func (k Key[V]) Create(ctx context.Context, m *Manager, input V) error {
	return k.write(ctx, m, nil, input, createValue)
}

// Ensure creates a missing setting using its descriptor's presentation. An
// existing value remains unchanged, including its version. A presentation still
// owned by the declaration is refreshed from it; one changed by Configure stays.
func (k Key[V]) Ensure(ctx context.Context, m *Manager, input V) error {
	return k.write(ctx, m, nil, input, ensureValue)
}

// EnsureIn applies Ensure within the caller's transaction through a savepoint.
// Parent rollback includes initialization and declaration-owned presentation changes.
func (k Key[V]) EnsureIn(ctx context.Context, tx *database.Tx, m *Manager, input V) error {
	if tx == nil {
		return invalid()
	}
	return k.write(ctx, m, tx, input, ensureValue)
}

// Upsert atomically creates or replaces the value/version. Existing presentation
// is preserved; Configure changes presentation explicitly.
func (k Key[V]) Upsert(ctx context.Context, m *Manager, input V) error {
	return k.write(ctx, m, nil, input, upsertValue)
}
func (k Key[V]) Set(ctx context.Context, m *Manager, input V) error {
	return k.write(ctx, m, nil, input, setValue)
}
func (k Key[V]) SetIn(ctx context.Context, tx *database.Tx, m *Manager, input V) error {
	if tx == nil {
		return invalid()
	}
	return k.write(ctx, m, tx, input, setValue)
}
func (k Key[V]) UpsertIn(ctx context.Context, tx *database.Tx, m *Manager, input V) error {
	if tx == nil {
		return invalid()
	}
	return k.write(ctx, m, tx, input, upsertValue)
}
func (k Key[V]) write(ctx context.Context, m *Manager, outer *database.Tx, input V, mode writeMode) error {
	if err := k.check(m); err != nil {
		return err
	}
	run := func(ctx context.Context, tx *database.Tx) error {
		if err := m.invalidateAfterCommit(tx, k.Name()); err != nil {
			return err
		}
		snapshot, err := extensionvalue.Encode(ctx, k.definition.codec, input)
		if err != nil {
			return err
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		q, f := store.Settings.Query(), store.Settings
		draft := []orm.Assignment[store.Setting]{store.Settings.Value.Set(snapshot), store.Settings.Version.Set(uint32(k.Version())), store.Settings.UpdatedAt.Set(now)}
		if mode == setValue {
			_, err := orm.Update(ctx, tx, q, store.Settings.Key(), string(k.Name()), draft...)
			return err
		}
		draft = append(presentationDraft(draft, k.definition.presentation), store.Settings.PresentationDeclared.Set(true), store.Settings.Name.Set(string(k.Name())), store.Settings.CreatedAt.Set(now))
		if mode == createValue {
			_, err := store.Settings.Insert(ctx, tx, draft...)
			return err
		}
		// Ensure refreshes only a declaration-owned presentation; the value,
		// version and an explicitly configured presentation stay unchanged.
		conflict := orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(orm.ConflictIncoming(f.Kind), orm.ConflictIncoming(f.Parameters), orm.ConflictIncoming(f.GroupName), orm.ConflictIncoming(f.Label), orm.ConflictIncoming(f.Description), orm.ConflictIncoming(f.SortOrder), orm.ConflictIncoming(f.IsPublic)).Where(f.PresentationDeclared.Eq(true))
		if mode == upsertValue {
			conflict = orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(orm.ConflictIncoming(f.Value), orm.ConflictIncoming(f.Version), orm.ConflictIncoming(f.UpdatedAt))
		}
		_, err = orm.Upsert(ctx, tx, q, conflict, draft...)
		return err
	}
	defer m.invalidate(k.Name())
	m.invalidate(k.Name())
	if outer != nil {
		return m.store.Join(ctx, outer, run)
	}
	return m.store.Write(ctx, run)
}

// invalidateAfterCommit covers writes joined to a caller's transaction, whose
// commit happens after this manager's call returns.
func (m *Manager) invalidateAfterCommit(tx *database.Tx, names ...Name) error {
	if m.cache == nil {
		return nil
	}
	return tx.AfterCommit(func(context.Context) error {
		m.invalidate(names...)
		return nil
	})
}
func presentationDraft(d []orm.Assignment[store.Setting], p Presentation) []orm.Assignment[store.Setting] {
	return append(d, store.Settings.Kind.Set(string(p.Kind)), store.Settings.Parameters.Set(p.Parameters), store.Settings.GroupName.Set(string(p.Group)), store.Settings.Label.Set(p.Label), store.Settings.Description.Set(p.Description), store.Settings.SortOrder.Set(p.Order), store.Settings.IsPublic.Set(p.Public))
}

// Configure explicitly takes ownership of the persisted presentation. Later
// declaration changes no longer replace it until ResetPresentation is called.
func (k Key[V]) Configure(ctx context.Context, m *Manager, p Presentation) error {
	if err := k.check(m); err != nil {
		return err
	}
	p, err := p.normalized()
	if err != nil {
		return err
	}
	return k.present(ctx, m, p, false)
}

// ResetPresentation restores the declared presentation and returns its
// ownership to the declaration, so Ensure and Reconcile keep it current.
func (k Key[V]) ResetPresentation(ctx context.Context, m *Manager) error {
	if err := k.check(m); err != nil {
		return err
	}
	return k.present(ctx, m, k.definition.presentation, true)
}
func (k Key[V]) present(ctx context.Context, m *Manager, p Presentation, declared bool) error {
	defer m.invalidate(k.Name())
	m.invalidate(k.Name())
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = orm.Update(ctx, tx, store.Settings.Query(), store.Settings.Key(), string(k.Name()), append(presentationDraft([]orm.Assignment[store.Setting]{}, p), store.Settings.PresentationDeclared.Set(declared), store.Settings.UpdatedAt.Set(now))...)
		return err
	})
}
func (k Key[V]) Remove(ctx context.Context, m *Manager) (bool, error) {
	if err := k.check(m); err != nil {
		return false, err
	}
	removed := false
	defer m.invalidate(k.Name())
	m.invalidate(k.Name())
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		q, f := store.Settings.Query(), store.Settings
		rows, err := orm.DeleteEach(ctx, tx, q.Where(f.Name.Eq(string(k.Name()))), 1)
		removed = len(rows) == 1
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
func (k Key[V]) Get(ctx context.Context, m *Manager) (value.Optional[V], error) {
	record, err := k.Find(ctx, m)
	if err != nil {
		return value.Optional[V]{}, err
	}
	r, ok := record.Get()
	if !ok {
		return value.Optional[V]{}, nil
	}
	decoded, err := k.Decode(ctx, r)
	if err != nil {
		return value.Optional[V]{}, err
	}
	return value.Set(decoded), nil
}

// GetOr uses a validated, independently decoded fallback only when no row
// exists. Invalid stored values or versions never silently become defaults.
// With Options.Cache, repeated calls are served from the manager's cache.
func (k Key[V]) GetOr(ctx context.Context, m *Manager, fallback V) (V, error) {
	result, err := k.Get(ctx, m)
	if err != nil {
		return *new(V), err
	}
	if v, ok := result.Get(); ok {
		return v, nil
	}
	return k.fallback(ctx, fallback)
}
func (k Key[V]) fallback(ctx context.Context, fallback V) (V, error) {
	snapshot, err := extensionvalue.Encode(ctx, k.definition.codec, fallback)
	if err != nil {
		return *new(V), err
	}
	return extensionvalue.Decode(ctx, k.definition.codec, snapshot)
}
