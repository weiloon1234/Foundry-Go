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
// existing value and its presentation remain unchanged, including their version.
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
		draft = presentationDraft(draft, k.definition.presentation).SetName(string(k.Name())).SetCreatedAt(now)
		if mode == createValue {
			_, err := q.Create(ctx, tx, draft)
			return err
		}
		conflict := query.OnConflict(f.Name).DoNothing()
		if mode == upsertValue {
			conflict = query.OnConflict(f.Name).Update(f.Value, f.Version, f.UpdatedAt)
		}
		_, err = q.Upsert(ctx, tx, draft, conflict)
		return err
	}
	if outer != nil {
		return m.store.Join(ctx, outer, run)
	}
	return m.store.Write(ctx, run)
}
func presentationDraft(d store.SettingDraft, p Presentation) store.SettingDraft {
	return d.SetKind(string(p.Kind)).SetParameters(p.Parameters).SetGroupName(string(p.Group)).SetLabel(p.Label).SetDescription(p.Description).SetSortOrder(p.Order).SetIsPublic(p.Public)
}
func (k Key[V]) Configure(ctx context.Context, m *Manager, p Presentation) error {
	if err := k.check(m); err != nil {
		return err
	}
	p, err := p.normalized()
	if err != nil {
		return err
	}
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundrySettings().Update(ctx, tx, string(k.Name()), presentationDraft(store.SettingDraft{}, p).SetUpdatedAt(now))
		return err
	})
}
func (k Key[V]) Remove(ctx context.Context, m *Manager) (bool, error) {
	if err := k.check(m); err != nil {
		return false, err
	}
	removed := false
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
func (k Key[V]) GetOr(ctx context.Context, m *Manager, fallback V) (V, error) {
	result, err := k.Get(ctx, m)
	if err != nil {
		return *new(V), err
	}
	if v, ok := result.Get(); ok {
		return v, nil
	}
	snapshot, err := extensionvalue.Encode(ctx, k.definition.codec, fallback)
	if err != nil {
		return *new(V), err
	}
	return extensionvalue.Decode(ctx, k.definition.codec, snapshot)
}
