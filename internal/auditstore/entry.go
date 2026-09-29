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
// Redaction records the sensitive-name policy that wrote the row. Sequence is
// the database-assigned insertion order. SubjectKey and ActorKey are generated
// lookup keys derived from Subject and Origin; they are never written directly.
// Registration never installs audit observers for this private storage model.
//
//foundry:model table=foundry_audit
type Entry struct {
	ID            model.ID[Entry]
	Sequence      int64 `foundry:"default=database"`
	Area          string
	Operation     lifecycle.Operation
	Action        string
	Version       uint32
	Subject       value.Nullable[value.JSON[model.Identity]]
	SubjectKey    value.Nullable[string] `foundry:"default=database"`
	Payload       value.JSON[json.RawMessage]
	Redacted      bool
	Redaction     uint8
	Origin        value.JSON[attribution.Origin]
	ActorKey      value.Nullable[string] `foundry:"default=database"`
	Correlation   value.Nullable[string]
	RequestMethod string
	RequestRoute  string
	CreatedAt     temporal.DateTime `foundry:"default=database"`
}

func (Entry) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("stored audit entry")) }

// Activity is the payload-free projection used by heterogeneous timelines.
//
//foundry:projection
type Activity struct {
	ID            model.ID[Entry]
	Sequence      int64
	Area          string
	Operation     lifecycle.Operation
	Action        string
	Version       uint32
	Subject       value.Nullable[value.JSON[model.Identity]]
	Origin        value.JSON[attribution.Origin]
	Correlation   value.Nullable[string]
	RequestMethod string
	RequestRoute  string
	CreatedAt     temporal.DateTime
}

func (Activity) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("stored audit activity")) }
