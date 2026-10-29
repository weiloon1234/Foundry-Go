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
		draft := []orm.Assignment[store.Setting]{orm.JSONSet(store.Settings.Value, snapshot), orm.NumberSet(store.Settings.Version, uint32(k.Version())), orm.TimeSet(store.Settings.UpdatedAt, now)}
		if mode == setValue {
			_, err := orm.Update(ctx, tx, q, store.Settings.Key(), string(k.Name()), draft...)
			return err
		}
		draft = append(presentationDraft(draft, k.definition.presentation), orm.ColumnSet(store.Settings.PresentationDeclared, true), orm.TextSet(store.Settings.Name, string(k.Name())), orm.TimeSet(store.Settings.CreatedAt, now))
		if mode == createValue {
			_, err := orm.Insert(ctx, tx, store.Settings.Table, draft...)
			return err
		}
		// Ensure refreshes only a declaration-owned presentation; the value,
		// version and an explicitly configured presentation stay unchanged.
		conflict := orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Name))).DoUpdate(orm.ConflictIncoming(orm.TextExpr(f.Kind)), orm.ConflictIncoming(orm.JSONExpr(f.Parameters)), orm.ConflictIncoming(orm.TextExpr(f.GroupName)), orm.ConflictIncoming(orm.TextExpr(f.Label)), orm.ConflictIncoming(orm.TextExpr(f.Description)), orm.ConflictIncoming(orm.NumberExpr(f.SortOrder)), orm.ConflictIncoming(orm.ColumnExpr(f.IsPublic))).Where(orm.ColumnEq(f.PresentationDeclared, true))
		if mode == upsertValue {
			conflict = orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Name))).DoUpdate(orm.ConflictIncoming(orm.JSONExpr(f.Value)), orm.ConflictIncoming(orm.NumberExpr(f.Version)), orm.ConflictIncoming(orm.TimeExpr(f.UpdatedAt)))
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
	return append(d, orm.TextSet(store.Settings.Kind, string(p.Kind)), orm.JSONSet(store.Settings.Parameters, p.Parameters), orm.TextSet(store.Settings.GroupName, string(p.Group)), orm.TextSet(store.Settings.Label, p.Label), orm.TextSet(store.Settings.Description, p.Description), orm.NumberSet(store.Settings.SortOrder, p.Order), orm.ColumnSet(store.Settings.IsPublic, p.Public))
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
		_, err = orm.Update(ctx, tx, store.Settings.Query(), store.Settings.Key(), string(k.Name()), append(presentationDraft([]orm.Assignment[store.Setting]{}, p), orm.ColumnSet(store.Settings.PresentationDeclared, declared), orm.TimeSet(store.Settings.UpdatedAt, now))...)
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
		rows, err := orm.DeleteEach(ctx, tx, q.Where(orm.TextEq(f.Name, string(k.Name()))), 1)
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
