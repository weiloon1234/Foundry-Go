package orm

import (
	"context"
	"encoding/json"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ModelIdentity retains a declaration's concrete primary key without model methods.
// Identity reads select keys only and do not run retrieval hooks. Example: orm.IdentityOf(Users.Table, Users.Key()).
type ModelIdentity[M any, K comparable] struct {
	_     [0]*M
	table *tableCore
	key   columnData[K]
}

// IdentityOf binds the model's own primary handle. Example: orm.IdentityOf(Users.Table, Users.Key()).
func IdentityOf[M any, K comparable](table Table[M, K], key Key[M, K]) ModelIdentity[M, K] {
	return ModelIdentity[M, K]{table: table.core, key: key.key}
}

// Validate rejects zero declarations and keys from another declaration. Example: identity.Validate().
func (d ModelIdentity[M, K]) Validate() error {
	if err := tableError(d.table); err != nil {
		return err
	}
	if d.key.core == nil || d.key.core != d.table.primary || d.key.core.sensitive {
		return invalid("model identity requires its declared non-sensitive primary key")
	}
	return nil
}

// ModelName returns the persisted table name. Example: identity.ModelName().
func (d ModelIdentity[M, K]) ModelName() string {
	if d.table == nil {
		return ""
	}
	return d.table.model.Table
}

// Reference builds a typed reference using the stored key codec. Example: identity.Reference(id).
func (d ModelIdentity[M, K]) Reference(key K) model.Reference[M, K] {
	if d.Validate() != nil {
		return model.Reference[M, K]{}
	}
	return model.NewReference[M](d.ModelName(), key, auditKeyCodec[K]{bind: d.key.bind, decode: d.key.decode})
}

// ReferenceOf reads the model's stored primary key without I/O. Example: identity.ReferenceOf(user).
func (d ModelIdentity[M, K]) ReferenceOf(m M) model.Reference[M, K] {
	if d.Validate() != nil {
		return model.Reference[M, K]{}
	}
	return d.Reference(*(*K)(unsafe.Add(unsafe.Pointer(&m), d.key.core.offset)))
}

// Parse restores a reference with the same model and codec. Example: identity.Parse(stored).
func (d ModelIdentity[M, K]) Parse(stored model.Identity) (model.Reference[M, K], error) {
	if err := d.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	return d.Reference(*new(K)).Parse(stored)
}

// KeyToken preserves SQL equality canonicalization for extension storage. Example: identity.KeyToken(id).
func (d ModelIdentity[M, K]) KeyToken(key K) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	var token string
	err := callback.Isolated("capture model primary key", func() error {
		raw, err := d.key.bind(key)
		if err != nil {
			return err
		}
		if raw == nil {
			return invalid("model identity key cannot be null")
		}
		encoded, err := querycore.RecordEqualityKey(d.key.core.kind, raw)
		if err != nil {
			return err
		}
		data, err := json.Marshal(encoded)
		token = string(data)
		return err
	})
	return token, err
}

// Scope retains model and key ownership for bounded membership. Example: Users.Query().Where(identity.Scope(ids...)).
func (d ModelIdentity[M, K]) Scope(keys ...K) Predicate[M] {
	if err := d.Validate(); err != nil {
		return Predicate[M]{err: err}
	}
	if len(keys) > query.MaxIdentityBatch {
		return Predicate[M]{err: invalid("model identity batch exceeds its limit")}
	}
	return comparison[M](d.key, querycore.In, keys)
}

// ActiveKeys selects currently visible keys in one bounded statement. Example: identity.ActiveKeys(ctx,db,ids).
func (d ModelIdentity[M, K]) ActiveKeys(ctx context.Context, ex database.Executor, keys []K) ([]K, error) {
	return identityKeys(ctx, ex, d, keys, false, false)
}

// RetainedKeys includes soft-deleted owners for orphan inspection. Example: identity.RetainedKeys(ctx,db,ids).
func (d ModelIdentity[M, K]) RetainedKeys(ctx context.Context, ex database.Executor, keys []K) ([]K, error) {
	return identityKeys(ctx, ex, d, keys, true, false)
}

// LockActive locks one visible owner in the caller's transaction. Example: identity.LockActive(ctx,tx,id).
func (d ModelIdentity[M, K]) LockActive(ctx context.Context, tx *database.Tx, key K) (bool, error) {
	if err := lockContext(ctx, tx); err != nil {
		return false, err
	}
	keys, err := identityKeys(ctx, tx, d, []K{key}, false, true)
	return len(keys) > 0, err
}
func identityKeys[M any, K comparable](ctx context.Context, ex database.Executor, d ModelIdentity[M, K], keys []K, retained, lock bool) ([]K, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if len(keys) > query.MaxIdentityBatch {
		return nil, invalid("model identity batch exceeds its limit")
	}
	if err := executionContext(ctx, ex); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []K{}, nil
	}
	q := Query[M]{state: newQuery(d.table)}.Where(d.Scope(keys...)).OrderBy(Key[M, K]{key: d.key}.Asc())
	if retained && d.table.model.SoftDelete != nil {
		q = q.WithTrashed()
	}
	q.state.ast.ScopeContext = ctx
	node := q.state.ast.ModelSelect(nil)
	node.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: d.ModelName(), Column: d.key.core.metadata.Name}}}
	if lock {
		node.Locks = []querycore.LockSpec{{Strength: querycore.LockUpdate}}
		node.Limit = value.Set(1)
	}
	statement, err := querycore.CompileRead(node, ctx, lock, nil, true)
	if err != nil {
		return nil, err
	}
	rows, err := database.ReadQuery(statement.Context(ctx), ex, statement.SQLText, statement.Bindings...)
	if err != nil {
		return nil, err
	}
	var result []K
	err = ormcore.Collect(ctx, rows, func(row database.Row) error {
		var key K
		if err := row.Scan(d.key.scanValue(&key)); err != nil {
			return err
		}
		result = append(result, key)
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// LegacyIdentityScope keeps extension predicates usable by generated consumers during migration.
// Example: legacyQuery.Where(orm.LegacyIdentityScope(identity,ids...)).
func LegacyIdentityScope[M any, K comparable](d ModelIdentity[M, K], keys ...K) query.Predicate[M] {
	if d.Validate() != nil || len(keys) > query.MaxIdentityBatch {
		return query.Predicate[M]{}
	}
	return query.FoundryPredicate[M](d.Scope(keys...).expression, sqlowner.Queries())
}
