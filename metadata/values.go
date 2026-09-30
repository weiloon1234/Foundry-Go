package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (k Key[M, K, V]) Set(ctx context.Context, m *Manager, owner model.Reference[M, K], input V) error {
	if err := k.check(m); err != nil {
		return err
	}
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error { return k.set(ctx, tx, m, owner, input) })
}

// SetIn joins a business transaction through the common savepoint/schema helper.
// A suppressed error cannot leave a partial metadata write in the outer Tx.
func (k Key[M, K, V]) SetIn(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], input V) error {
	if err := k.check(m); err != nil {
		return err
	}
	return m.store.Join(ctx, tx, func(ctx context.Context, tx *database.Tx) error { return k.set(ctx, tx, m, owner, input) })
}
func (k Key[M, K, V]) set(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], input V) error {
	snapshot, err := extensionvalue.Encode(ctx, k.definition.value, input)
	if err != nil {
		return err
	}
	subject, err := k.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
	if err != nil {
		return err
	}
	now, err := m.store.Now()
	if err != nil {
		return err
	}
	id := extensions.Digest(subject.Scope, subject.Key, string(k.Name()))
	q, f := store.QueryFoundryModelMetadata(), store.MetaFields()
	existing, err := q.Where(f.Key.Eq(id)).First(ctx, tx)
	if err != nil {
		return err
	}
	if row, ok := existing.Get(); ok {
		if err := k.checkRow(extensionrow.ForSubject(k.definition.owner, subject), row, subject.Key); err != nil {
			return err
		}
		_, err = q.Update(ctx, tx, id, store.MetaDraft{}.SetValue(snapshot).SetVersion(uint32(k.Version())).SetUpdatedAt(now))
		return err
	}
	count, err := q.Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key)).Count(ctx, tx)
	if err != nil {
		return err
	}
	if count >= MaxKeysPerOwner {
		return fault.New(fault.Conflict, "model metadata key limit reached")
	}
	_, err = q.Create(ctx, tx, store.MetaDraft{}.SetKey(id).SetOwner(string(k.definition.owner.Name())).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetIdentity(subject.Identity).SetName(string(k.Name())).SetVersion(uint32(k.Version())).SetValue(snapshot).SetCreatedAt(now).SetUpdatedAt(now))
	return err
}

// checkRow validates row as this key's stored value for the subject key
// expected, through the shared persisted-identity check.
func (k Key[M, K, V]) checkRow(rows extensionrow.Rows[M, K], row store.Meta, expected string) error {
	if row.SubjectKey != expected || row.Name != string(k.Name()) {
		return invalid()
	}
	return rows.Validate(rowIdentity(row), row.Name)
}

// Batch is an immutable snapshot of one typed key for currently active owners.
// Decode returns fresh values. It is not an authorization or eligibility cache
// for later operations; each new read/write rechecks the actual owner.
type Batch[M any, K comparable, V any] struct {
	key    Key[M, K, V]
	active map[string]bool
	values map[string]value.JSON[json.RawMessage]
}

func (Batch[M, K, V]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("metadata batch")) }
func (b Batch[M, K, V]) Get(ctx context.Context, owner model.Reference[M, K]) (value.Optional[V], error) {
	if ctx == nil || b.active == nil || b.values == nil {
		return value.Optional[V]{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return value.Optional[V]{}, err
	}
	subject, err := b.key.definition.owner.SubjectKey(owner)
	if err != nil {
		return value.Optional[V]{}, err
	}
	if !b.active[subject] {
		return value.Optional[V]{}, database.NotFound
	}
	snapshot, ok := b.values[subject]
	if !ok {
		return value.Optional[V]{}, nil
	}
	decoded, err := extensionvalue.Decode(ctx, b.key.definition.value, snapshot)
	if err != nil {
		return value.Optional[V]{}, err
	}
	return value.Set(decoded), nil
}
func (k Key[M, K, V]) Load(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (Batch[M, K, V], error) {
	if err := k.check(m); err != nil {
		return Batch[M, K, V]{}, err
	}
	var result Batch[M, K, V]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = k.loadIn(ctx, tx, m, owners)
		return err
	})
	if err != nil {
		return Batch[M, K, V]{}, err
	}
	return result, nil
}

// loadIn reads one owner batch inside tx, which is either the store's own
// read-only snapshot or a savepoint joined to the caller's transaction.
func (k Key[M, K, V]) loadIn(ctx context.Context, tx *database.Tx, m *Manager, owners []model.Reference[M, K]) (Batch[M, K, V], error) {
	active, err := k.definition.owner.Active(ctx, tx, m.store.Registry(), owners)
	if err != nil {
		return Batch[M, K, V]{}, err
	}
	result := Batch[M, K, V]{key: k, active: active, values: make(map[string]value.JSON[json.RawMessage])}
	if len(active) == 0 {
		return result, nil
	}
	checked, err := extensionrow.For(k.definition.owner, owners)
	if err != nil {
		return Batch[M, K, V]{}, err
	}
	keys := make([]string, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	f := store.MetaFields()
	var budget extensionvalue.BatchBudget
	err = store.QueryFoundryModelMetadata().Where(f.Scope.Eq(k.definition.owner.Scope()), f.SubjectKey.In(keys...), f.Name.Eq(string(k.Name()))).Each(ctx, tx, func(row store.Meta) error {
		if !active[row.SubjectKey] {
			return invalid()
		}
		if err := k.checkRow(checked, row, row.SubjectKey); err != nil {
			return err
		}
		if row.Version != uint32(k.Version()) {
			return fault.New(fault.Conflict, "stored metadata version differs from its descriptor")
		}
		if err := budget.Add(row.Value); err != nil {
			return err
		}
		if _, err := extensionvalue.Decode(ctx, k.definition.value, row.Value); err != nil {
			return err
		}
		result.values[row.SubjectKey] = row.Value
		return nil
	})
	if err != nil {
		return Batch[M, K, V]{}, err
	}
	return result, nil
}
func (k Key[M, K, V]) Get(ctx context.Context, m *Manager, owner model.Reference[M, K]) (value.Optional[V], error) {
	batch, err := k.Load(ctx, m, []model.Reference[M, K]{owner})
	if err != nil {
		return value.Optional[V]{}, err
	}
	return batch.Get(ctx, owner)
}
func (k Key[M, K, V]) Forget(ctx context.Context, m *Manager, owner model.Reference[M, K]) (bool, error) {
	if err := k.check(m); err != nil {
		return false, err
	}
	removed := false
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		removed, err = k.forget(ctx, tx, m, owner)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// forget removes the key after locking the owner in tx.
func (k Key[M, K, V]) forget(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K]) (bool, error) {
	subject, err := k.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
	if err != nil {
		return false, err
	}
	q, f := store.QueryFoundryModelMetadata(), store.MetaFields()
	id := extensions.Digest(subject.Scope, subject.Key, string(k.Name()))
	found, err := q.Where(f.Key.Eq(id)).First(ctx, tx)
	if err != nil {
		return false, err
	}
	row, ok := found.Get()
	if !ok {
		return false, nil
	}
	if err := k.checkRow(extensionrow.ForSubject(k.definition.owner, subject), row, subject.Key); err != nil {
		return false, err
	}
	if _, err := q.Delete(ctx, tx, id); err != nil {
		return false, err
	}
	return true, nil
}
