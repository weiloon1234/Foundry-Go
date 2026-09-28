// Package extensionstore owns ordinary generated infrastructure models for
// typed metadata, translations and settings. Public packages own their APIs.
package extensionstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_model_metadata primary=Key
type Meta struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Name       string
	Version    uint32
	Value      value.JSON[json.RawMessage]
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Meta) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model metadata row")) }
