// Package extensions shares typed model ownership for framework model stores.
// It does not grant authentication or authorization; applications retain those
// decisions before invoking a model extension.
package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type OwnerName string
type declarationID struct{ nonzero byte }

// Owner binds one model and its concrete key to generated identity metadata.
// Reuse this exact declaration across attachments, metadata and translations.
type Owner[M any, K comparable] struct{ definition *ownerDefinition[M, K] }
type ownerDefinition[M any, K comparable] struct {
	name   OwnerName
	source query.ModelIdentity[M, K]
	id     *declarationID
}

func DefineOwner[M any, K comparable](name OwnerName, source query.ModelIdentity[M, K]) Owner[M, K] {
	return Owner[M, K]{definition: &ownerDefinition[M, K]{name: name, source: source, id: &declarationID{}}}
}
func (o Owner[M, K]) Validate() error {
	if o.definition == nil || !identifier.Semantic(string(o.definition.name)) || reflect.TypeFor[M]().Kind() != reflect.Struct {
		return invalid("invalid model extension owner")
	}
	return o.definition.source.Validate()
}
func (o Owner[M, K]) Name() OwnerName {
	if o.definition == nil {
		return ""
	}
	return o.definition.name
}
func (o Owner[M, K]) ModelName() string {
	if o.definition == nil {
		return ""
	}
	return o.definition.source.ModelName()
}
func (o Owner[M, K]) Scope() string { return Digest(string(o.Name()), o.ModelName()) }
func (o Owner[M, K]) Reference(key K) model.Reference[M, K] {
	if o.definition == nil {
		return model.Reference[M, K]{}
	}
	return o.definition.source.Reference(key)
}
func (o Owner[M, K]) Parse(identity model.Identity) (model.Reference[M, K], error) {
	if err := o.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	return o.definition.source.Parse(identity)
}
func (o Owner[M, K]) QueryScope(keys ...K) query.Predicate[M] {
	if o.definition == nil {
		return query.Predicate[M]{}
	}
	return o.definition.source.Scope(keys...)
}
func (o Owner[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model extension owner")) }

// Subject is frozen infrastructure metadata, not owner authority. It contains
// the full typed identity at an explicit JSON storage boundary and an opaque
// equality key shared by every model extension.
type Subject struct {
	Scope, Key string
	Identity   value.JSON[model.Identity]
}

func (Subject) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model extension subject")) }

func (o Owner[M, K]) Subject(reference model.Reference[M, K]) (Subject, error) {
	if err := o.Validate(); err != nil {
		return Subject{}, err
	}
	identity, err := reference.Identity()
	if err != nil {
		return Subject{}, err
	}
	parsed, err := o.Parse(identity)
	if err != nil {
		return Subject{}, err
	}
	token, err := o.definition.source.KeyToken(parsed.Key())
	if err != nil {
		return Subject{}, err
	}
	snapshot, err := value.NewJSON(identity)
	if err != nil {
		return Subject{}, err
	}
	return Subject{Scope: o.Scope(), Key: Digest(token), Identity: snapshot}, nil
}

// Lock validates the exact registry declaration then locks a currently active
// owner. The caller's transaction serializes extension writes against owner
// deletion and soft deletion. Existing model snapshots alone are insufficient.
func (o Owner[M, K]) Lock(ctx context.Context, tx *database.Tx, registry *Registry, reference model.Reference[M, K]) (Subject, error) {
	if err := o.Check(registry); err != nil {
		return Subject{}, err
	}
	subject, err := o.Subject(reference)
	if err != nil {
		return Subject{}, err
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return Subject{}, err
	}
	parsed, err := o.Parse(identity)
	if err != nil {
		return Subject{}, err
	}
	found, err := o.definition.source.LockActive(ctx, tx, parsed.Key())
	if err != nil {
		return Subject{}, err
	}
	if !found {
		return Subject{}, database.NotFound
	}
	return subject, nil
}

// Active performs one primary-key-only query for a bounded batch and returns
// subjects only for owners still visible under the generated soft-delete policy.
// The returned map keys are opaque Subject.Key values, not application IDs.
func (o Owner[M, K]) Active(ctx context.Context, executor database.Executor, registry *Registry, references []model.Reference[M, K]) (map[string]Subject, error) {
	if err := o.Check(registry); err != nil {
		return nil, err
	}
	if len(references) > query.MaxIdentityBatch {
		return nil, invalid("owner batch exceeds its limit")
	}
	keys := make([]K, len(references))
	for i, ref := range references {
		identity, err := ref.Identity()
		if err != nil {
			return nil, err
		}
		parsed, err := o.Parse(identity)
		if err != nil {
			return nil, err
		}
		keys[i] = parsed.Key()
	}
	found, err := o.definition.source.ActiveKeys(ctx, executor, keys)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Subject, len(found))
	for _, key := range found {
		s, err := o.Subject(o.Reference(key))
		if err != nil {
			return nil, err
		}
		result[s.Key] = s
	}
	return result, nil
}

// Digest is the shared unambiguous hashing boundary for infrastructure scopes
// and operation identifiers. Inputs are JSON-escaped owned strings.
func Digest(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func invalid(message string) error { return fault.New(fault.Invalid, message) }
