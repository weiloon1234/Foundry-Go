package settings

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
		q, f := store.QueryFoundrySettings(), store.SettingFields()
		draft := store.SettingDraft{}.SetValue(snapshot).SetVersion(uint32(k.Version())).SetUpdatedAt(now)
		if mode == setValue {
			_, err := q.Update(ctx, tx, string(k.Name()), draft)
			return err
		}
		draft = presentationDraft(draft, k.definition.presentation).SetPresentationDeclared(true).SetName(string(k.Name())).SetCreatedAt(now)
		if mode == createValue {
			_, err := q.Create(ctx, tx, draft)
			return err
		}
		// Ensure refreshes only a declaration-owned presentation; the value,
		// version and an explicitly configured presentation stay unchanged.
		conflict := query.OnConflict(f.Name).Update(f.Kind, f.Parameters, f.GroupName, f.Label, f.Description, f.SortOrder, f.IsPublic).Where(f.PresentationDeclared.Eq(true))
		if mode == upsertValue {
			conflict = query.OnConflict(f.Name).Update(f.Value, f.Version, f.UpdatedAt)
		}
		_, err = q.Upsert(ctx, tx, draft, conflict)
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
func presentationDraft(d store.SettingDraft, p Presentation) store.SettingDraft {
	return d.SetKind(string(p.Kind)).SetParameters(p.Parameters).SetGroupName(string(p.Group)).SetLabel(p.Label).SetDescription(p.Description).SetSortOrder(p.Order).SetIsPublic(p.Public)
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
		_, err = store.QueryFoundrySettings().Update(ctx, tx, string(k.Name()), presentationDraft(store.SettingDraft{}, p).SetPresentationDeclared(declared).SetUpdatedAt(now))
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
		q, f := store.QueryFoundrySettings(), store.SettingFields()
		rows, err := q.Where(f.Name.Eq(string(k.Name()))).DeleteEach(ctx, tx, 1)
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
