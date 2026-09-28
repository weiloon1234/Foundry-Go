package query

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ModelIdentity is a typed primary-key descriptor for framework integrations.
// It reuses a generated model query's table, key codec and soft-delete policy.
// Identity lookups select only stored keys: model getters/retrieval observers do
// not run, and caller-supplied model snapshots do not establish existence.
type ModelIdentity[M any, K comparable] struct {
	query   Query[M]
	primary ScalarField[M, K]
}

// IdentityOf requires a bare generated query and its own declared primary field.
// Construction performs no I/O. Validate reports mismatched manual declarations.
func IdentityOf[M any, K comparable](q Query[M], primary KeyField[M, K]) ModelIdentity[M, K] {
	if nilDescriptor(primary) {
		return ModelIdentity[M, K]{}
	}
	return ModelIdentity[M, K]{query: q, primary: primary.relationKey()}
}
func (d ModelIdentity[M, K]) Validate() error {
	if d.query.definition == nil || d.query.hasQueryOptions() {
		return fault.New(fault.Invalid, "model identity requires a bare generated query")
	}
	if err := d.query.Validate(); err != nil {
		return err
	}
	if err := d.primary.ref.validate(d.query.table); err != nil {
		return err
	}
	if err := d.primary.codec.Validate(); err != nil {
		return err
	}
	field, ok := d.query.definition.modelField(d.primary.ref.column)
	if !ok || d.primary.ref.column != d.query.definition.primary || field.typ != reflect.TypeFor[K]() || field.kind != d.primary.codec.ParameterType() || field.sensitive || d.primary.codec.SensitiveValues() {
		return fault.New(fault.Invalid, "model identity requires its declared non-sensitive primary field")
	}
	return nil
}
func (d ModelIdentity[M, K]) ModelName() string { return d.query.table }
func (d ModelIdentity[M, K]) Reference(key K) model.Reference[M, K] {
	return model.NewReference[M](d.query.table, key, d.primary.codec)
}
func (d ModelIdentity[M, K]) Parse(identity model.Identity) (model.Reference[M, K], error) {
	if err := d.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	return d.Reference(*new(K)).Parse(identity)
}

// KeyToken captures the primary key's database equality representation. It is
// opaque persistence metadata, not a public model ID. Shared interval and signed
// zero canonicalization prevents equivalent SQL keys getting separate stores.
func (d ModelIdentity[M, K]) KeyToken(key K) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	var token string
	err := callback.Isolated("capture model primary key", func() error {
		raw, err := d.primary.codec.Bind(key)
		if err != nil {
			return err
		}
		if raw == nil {
			return fault.New(fault.Invalid, "model identity key cannot be null")
		}
		field, _ := d.query.definition.modelField(d.primary.ref.column)
		encoded, err := field.equalityKey(raw)
		if err != nil {
			return err
		}
		data, err := json.Marshal(encoded)
		if err != nil {
			return err
		}
		token = string(data)
		return nil
	})
	return token, err
}

const MaxIdentityBatch = 1000

func (d ModelIdentity[M, K]) reader(keys []K, retained, lock bool) readResult[K] {
	r := readResult[K]{err: d.Validate(), transaction: lock}
	if r.err != nil {
		return r
	}
	if len(keys) > MaxIdentityBatch {
		r.err = fault.New(fault.Invalid, "model identity batch exceeds its limit")
		return r
	}
	q := d.query.Where(d.primary.In(keys...)).OrderBy(d.primary.Asc())
	if retained && q.hasSoftDeletes() {
		q = q.WithTrashed()
	}
	r.node = q.modelSelect()
	r.node.selections = []selectItem{{expression: d.primary.ref}}
	if lock {
		r.node.locks = []lockSpec{{strength: lockUpdate}}
	}
	r.scan = func(row database.Row) (K, error) {
		var key K
		err := row.Scan(d.primary.codec.Scan(&key))
		return key, err
	}
	return r
}

// ActiveKeys performs one bounded query, excluding soft-deleted owners. Empty
// input still validates the descriptor and context but does no database I/O.
func (d ModelIdentity[M, K]) ActiveKeys(ctx context.Context, executor database.Executor, keys []K) ([]K, error) {
	return d.keys(ctx, executor, keys, false)
}

// RetainedKeys includes soft-deleted owners for explicit orphan inspection. It
// must not be used as the public read/write eligibility check.
func (d ModelIdentity[M, K]) RetainedKeys(ctx context.Context, executor database.Executor, keys []K) ([]K, error) {
	return d.keys(ctx, executor, keys, true)
}
func (d ModelIdentity[M, K]) keys(ctx context.Context, executor database.Executor, keys []K, retained bool) ([]K, error) {
	r := d.reader(keys, retained, false)
	if r.err != nil {
		return nil, r.err
	}
	if err := executionContext(ctx, executor); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []K{}, nil
	}
	return r.All(ctx, executor)
}

// LockActive prevents owner deletion, soft deletion or primary-key changes for
// the remainder of the caller's transaction. It does not establish authorization.
func (d ModelIdentity[M, K]) LockActive(ctx context.Context, tx *database.Tx, key K) (bool, error) {
	if err := lockContext(ctx, tx); err != nil {
		return false, err
	}
	r := d.reader([]K{key}, false, true)
	result, err := r.First(ctx, tx)
	return result.IsSet(), err
}

// Scope returns a model-owned membership predicate from bounded concrete keys.
// It composes with the ordinary query's other filters and deletion policy.
func (d ModelIdentity[M, K]) Scope(keys ...K) Predicate[M] {
	if err := d.Validate(); err != nil {
		return Predicate[M]{}
	}
	if len(keys) > MaxIdentityBatch {
		return Predicate[M]{}
	}
	return d.primary.In(keys...)
}
