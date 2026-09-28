// Package auditstore owns the heterogeneous audit persistence boundary. Public
// consumers read typed audit records; this infrastructure model is not a DTO.
package auditstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Entry stores either a model operation or a versioned domain action. Model
// capture owns field redaction; domain capture owns its document redaction flag.
// Registration never installs audit observers for this private storage model.
//
//foundry:model table=foundry_audit
type Entry struct {
	ID        model.ID[Entry]
	Area      string
	Operation lifecycle.Operation
	Action    string
	Version   uint32
	Subject   value.Nullable[value.JSON[model.Identity]]
	Payload   value.JSON[json.RawMessage]
	Redacted  bool
	Origin    value.JSON[attribution.Origin]
	CreatedAt temporal.DateTime `foundry:"default=database"`
}

func (Entry) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("stored audit entry")) }
