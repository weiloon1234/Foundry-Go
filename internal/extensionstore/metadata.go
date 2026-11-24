// Package extensionstore owns ordinary declared infrastructure models for
// typed metadata, translations and settings. Public packages own their APIs.
package extensionstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_model_metadata var=Metadata key=Key identity=false
type Meta struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Metadata.Query()).
	//
	// KeyColumn binds Meta.Key.
	// Example: orm.Set(Metadata.KeyColumn, key).
	Key string `foundry:"member=KeyColumn"`
	// Owner binds Meta.Owner.
	// Example: orm.Set(Metadata.Owner, owner).
	Owner string
	// Scope binds Meta.Scope.
	// Example: orm.Set(Metadata.Scope, scope).
	Scope string
	// SubjectKey binds Meta.SubjectKey.
	// Example: orm.Set(Metadata.SubjectKey, subjectKey).
	SubjectKey string
	// Identity binds Meta.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Metadata.Identity, encoded) }.
	Identity value.JSON[model.Identity]
	// Name binds Meta.Name.
	// Example: orm.Set(Metadata.Name, name).
	Name string
	// Version binds Meta.Version.
	// Example: orm.Set(Metadata.Version, version).
	Version uint32
	// Value binds Meta.Value.
	// Example: encoded, err := value.NewJSON(value); if err == nil { _ = orm.Set(Metadata.Value, encoded) }.
	Value value.JSON[json.RawMessage]
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Metadata.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Metadata.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (Meta) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model metadata row")) }
