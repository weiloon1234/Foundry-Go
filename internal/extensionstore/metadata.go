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

type Meta struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Name       string
	Version    uint32
	Value      value.JSON[json.RawMessage]
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
}

func (Meta) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model metadata row")) }
