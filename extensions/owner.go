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
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type OwnerName string
type declarationID struct{ nonzero byte }

// Owner binds one model and its concrete key to generated identity metadata.
// Reuse this exact declaration across attachments, metadata and translations.
type Owner[M any, K comparable] struct{ definition *ownerDefinition[M, K] }
type ownerDefinition[M any, K comparable] struct {
	name     OwnerName
	source   query.ModelIdentity[M, K]
	storage  string
	previous []string
	id       *declarationID
}

// OwnerOptions declares the owner's persisted identity. StorageModel is the
// model name recorded in the owner scope, so every stored extension row,
// registration key and attachment object path depends on the declared owner
// identity rather than the current table name. Empty uses the generated table
// name, which is the historical, compatible scope. Declare the previous table
// name here before renaming an owner table; stored identities attributed to it
// are then accepted and decoded through the current key codec.
//
// PreviousModels lists model names this owner's rows were recorded under before
// an undeclared rename. Only rows whose recorded scope and identity name one of
// these, the current model or the storage model are adopted by the explicit
// re-scope maintenance commands; rows of any other earlier model are reported,
// never moved.
type OwnerOptions struct {
	StorageModel   string
	PreviousModels []string
}

func DefineOwner[M any, K comparable](name OwnerName, source query.ModelIdentity[M, K]) Owner[M, K] {
	return DefineOwnerWith(name, source, OwnerOptions{})
}

// DefineOwnerWith declares an owner with an explicit persisted identity. Keep
// StorageModel unchanged for the lifetime of the stored data.
func DefineOwnerWith[M any, K comparable](name OwnerName, source query.ModelIdentity[M, K], options OwnerOptions) Owner[M, K] {
	return Owner[M, K]{definition: &ownerDefinition[M, K]{name: name, source: source, storage: options.StorageModel, previous: slices.Clone(options.PreviousModels), id: &declarationID{}}}
}
func (o Owner[M, K]) Validate() error {
	if o.definition == nil || !identifier.Semantic(string(o.definition.name)) || reflect.TypeFor[M]().Kind() != reflect.Struct {
		return invalid("invalid model extension owner")
	}
	if o.definition.storage != "" && !sqlname.Table(o.definition.storage) {
		return invalid("invalid model extension owner storage model")
	}
	if len(o.definition.previous) > maxPreviousModels {
		return invalid("too many previous model extension owner models")
	}
	for i, previous := range o.definition.previous {
		if !sqlname.Table(previous) || previous == o.ModelName() || previous == o.StorageModel() || slices.Contains(o.definition.previous[:i], previous) {
			return invalid("invalid previous model extension owner model")
		}
	}
	return o.definition.source.Validate()
}

const maxPreviousModels = 16

// RecordedModels lists every model name persisted identities of this owner may
// carry: the current model, the storage model and declared previous models.
func (o Owner[M, K]) RecordedModels() []string {
	if o.definition == nil {
		return nil
	}
	models := []string{o.ModelName()}
	if storage := o.StorageModel(); storage != models[0] {
		models = append(models, storage)
	}
	return append(models, o.definition.previous...)
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

// StorageModel is the declared model name of the persisted owner scope. It
// equals ModelName unless OwnerOptions declared a stable storage identity.
func (o Owner[M, K]) StorageModel() string {
	if o.definition == nil {
		return ""
	}
	if o.definition.storage != "" {
		return o.definition.storage
	}
	return o.definition.source.ModelName()
}

// Scope is the opaque persisted owner namespace. It depends only on the owner
// name and declared storage model, so a pinned storage model survives renames.
func (o Owner[M, K]) Scope() string { return Digest(string(o.Name()), o.StorageModel()) }
func (o Owner[M, K]) Reference(key K) model.Reference[M, K] {
	if o.definition == nil {
		return model.Reference[M, K]{}
	}
	return o.definition.source.Reference(key)
}

// Parse restores a typed reference from a serialized identity. An identity
// attributed to the declared storage model is accepted as this owner's model:
// persisted rows written before a table rename keep their original attribution,
// while the key must still round trip through the current key codec.
func (o Owner[M, K]) Parse(identity model.Identity) (model.Reference[M, K], error) {
	if err := o.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	if current := o.ModelName(); identity.ModelName() != current && identity.ModelName() == o.StorageModel() {
		renamed, err := identity.WithModelName(current)
		if err != nil {
			return model.Reference[M, K]{}, err
		}
		identity = renamed
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
