package metadata

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Record preserves model ownership while supporting deliberate administrative
// inspection. Decode through its matching typed Key, or explicitly export the
// dynamic value. Records do not implicitly serialize private metadata.
type Record[M any] struct {
	name             Name
	version          Version
	scope            string
	data             value.JSON[json.RawMessage]
	created, updated temporal.DateTime
	_                [0]*M
}

func (r Record[M]) Name() Name                             { return r.name }
func (r Record[M]) Version() Version                       { return r.version }
func (r Record[M]) CreatedAt() temporal.DateTime           { return r.created }
func (r Record[M]) UpdatedAt() temporal.DateTime           { return r.updated }
func (r Record[M]) DynamicValue() (json.RawMessage, error) { return r.data.Decode() }
func (Record[M]) Format(s fmt.State, _ rune)               { _, _ = s.Write([]byte("model metadata record")) }
func (Record[M]) MarshalJSON() ([]byte, error)             { return nil, invalid() }
func (k Key[M, K, V]) Decode(ctx context.Context, r Record[M]) (V, error) {
	if err := k.Validate(); err != nil {
		return *new(V), err
	}
	if r.scope != k.definition.owner.Scope() || r.name != k.Name() || r.version != k.Version() {
		return *new(V), invalid()
	}
	return extensionvalue.Decode(ctx, k.definition.value, r.data)
}
func record[M any](row store.Meta) Record[M] {
	return Record[M]{name: Name(row.Name), version: Version(row.Version), scope: row.Scope, data: row.Value, created: row.CreatedAt, updated: row.UpdatedAt}
}

// All returns the bounded complete value set for one active owner. It includes
// unregistered persisted keys for explicit migration/administrative inspection.
func All[M any, K comparable](ctx context.Context, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K]) ([]Record[M], error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := owner.Check(m.store.Registry()); err != nil {
		return nil, err
	}
	var result []Record[M]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := owner.SubjectKey(reference)
		if err != nil {
			return err
		}
		active, err := owner.Active(ctx, tx, m.store.Registry(), []model.Reference[M, K]{reference})
		if err != nil {
			return err
		}
		if !active[subject] {
			return database.NotFound
		}
		rows, err := extensionrow.For(owner, []model.Reference[M, K]{reference})
		if err != nil {
			return err
		}
		f := store.MetaFields()
		var budget extensionvalue.BatchBudget
		return store.QueryFoundryModelMetadata().Where(f.Scope.Eq(owner.Scope()), f.SubjectKey.Eq(subject)).OrderBy(f.Name.Asc()).Limit(MaxKeysPerOwner+1).Each(ctx, tx, func(row store.Meta) error {
			if len(result) >= MaxKeysPerOwner {
				return invalid()
			}
			if err := validateOwnerRow(rows, row); err != nil {
				return err
			}
			if err := budget.Add(row.Value); err != nil {
				return err
			}
			result = append(result, record[M](row))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func validateOwnerRow[M any, K comparable](rows extensionrow.Rows[M, K], row store.Meta) error {
	if !identifier.Semantic(row.Name) || row.Version == 0 {
		return invalid()
	}
	return rows.Validate(rowIdentity(row), row.Name)
}

// Matching yields a typed query predicate for owners with this exact canonical
// JSON value. It is a bounded materialized scope; the subsequent model query
// retains its own current visibility, filters and authorization constraints.
func (k Key[M, K, V]) Matching(ctx context.Context, m *Manager, input V) (query.Predicate[M], error) {
	if err := k.check(m); err != nil {
		return query.Predicate[M]{}, err
	}
	var keys []K
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		snapshot, err := extensionvalue.Encode(ctx, k.definition.value, input)
		if err != nil {
			return err
		}
		f := store.MetaFields()
		rows, err := store.MetadataIndex(store.QueryFoundryModelMetadata().Where(f.Scope.Eq(k.definition.owner.Scope()), f.Name.Eq(string(k.Name())), f.Version.Eq(uint32(k.Version())), f.Value.Eq(snapshot)).OrderBy(f.Key.Asc()).Limit(query.MaxIdentityBatch+1)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) > query.MaxIdentityBatch {
			return fault.New(fault.Conflict, "metadata matching scope exceeds its limit")
		}
		for _, row := range rows {
			identity, err := maintenanceIdentity(m.store.Registry(), k.definition.owner.Name(), k.definition.owner.Scope(), row)
			if err != nil {
				return err
			}
			ref, err := k.definition.owner.Parse(identity)
			if err != nil {
				return err
			}
			keys = append(keys, ref.Key())
		}
		return nil
	})
	if err != nil {
		return query.Predicate[M]{}, err
	}
	return k.definition.owner.QueryScope(keys...), nil
}

// DeleteAll removes metadata for an active owner. Lifecycle cleanup uses Cleanup
// instead, after the owner's ordinary DELETE/ForceDelete SQL has succeeded.
func DeleteAll[M any, K comparable](ctx context.Context, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K]) (int, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	count := 0
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := owner.Lock(ctx, tx, m.store.Registry(), reference)
		if err != nil {
			return err
		}
		count, err = deleteSubject(ctx, tx, subject)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func deleteSubject(ctx context.Context, tx *database.Tx, subject extensions.Subject) (int, error) {
	f := store.MetaFields()
	rows, err := store.QueryFoundryModelMetadata().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key)).DeleteEach(ctx, tx, MaxKeysPerOwner)
	return len(rows), err
}

// Cleanup joins a generated Deleted/ForceDeleted observer's transaction. Soft
// deletion keeps values for restoration; ordinary reads remain unavailable.
// A still-existing owner is rejected, preventing accidental early cleanup. Bulk
// model deletes skip observers and require explicit maintenance afterward.
func Cleanup[M any, K comparable](ctx context.Context, tx *database.Tx, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K], operation lifecycle.Operation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := owner.Check(m.store.Registry()); err != nil {
		return err
	}
	if operation == lifecycle.SoftDelete {
		return nil
	}
	if operation != lifecycle.Delete && operation != lifecycle.ForceDelete {
		return invalid()
	}
	subject, err := owner.Subject(reference)
	if err != nil {
		return err
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return err
	}
	return cleanupSubject(ctx, tx, m, owner.Name(), subject, identity)
}

// CleanupIdentity is Cleanup for an owner known by its registered name and
// persisted identity, such as a durable cleanup job's request. It joins tx and
// rejects an owner that still exists.
func CleanupIdentity(ctx context.Context, tx *database.Tx, m *Manager, owner extensions.OwnerName, identity model.Identity) error {
	if err := m.Validate(); err != nil {
		return err
	}
	subject, err := m.store.Registry().Subject(owner, identity)
	if err != nil {
		return err
	}
	return cleanupSubject(ctx, tx, m, owner, subject, identity)
}
func cleanupSubject(ctx context.Context, tx *database.Tx, m *Manager, owner extensions.OwnerName, subject extensions.Subject, identity model.Identity) error {
	return m.store.Join(ctx, tx, func(ctx context.Context, child *database.Tx) error {
		retained, err := m.store.Registry().RetainedSubjects(ctx, child, owner, []model.Identity{identity})
		if err != nil {
			return err
		}
		if retained[subject.Key] {
			return fault.New(fault.Conflict, "metadata cleanup requires a deleted owner")
		}
		_, err = deleteSubject(ctx, child, subject)
		return err
	})
}
