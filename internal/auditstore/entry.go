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
//foundry:model table=foundry_audit var=Entries key=ID identity=false
type Entry struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Entries.Query()).
	//
	// ID binds Entry.ID.
	// Example: orm.Set(Entries.ID, id).
	ID model.ID[Entry] `foundry:"member=ID"`
	// Sequence binds Entry.Sequence. Omission retains the database default.
	// Example: orm.Set(Entries.Sequence, sequence).
	Sequence int64 `foundry:"default=database"`
	// Area binds Entry.Area.
	// Example: orm.Set(Entries.Area, area).
	Area string
	// Operation binds Entry.Operation.
	// Example: orm.Set(Entries.Operation, operation).
	Operation lifecycle.Operation `foundry:"role=column"`
	// Action binds Entry.Action.
	// Example: orm.Set(Entries.Action, action).
	Action string
	// Version binds Entry.Version.
	// Example: orm.Set(Entries.Version, version).
	Version uint32
	// Subject binds Entry.Subject.
	// Example: orm.SetNull(Entries.Subject).
	Subject value.Nullable[value.JSON[model.Identity]]
	// SubjectKey binds Entry.SubjectKey. Omission retains the database default.
	// Example: orm.SetNull(Entries.SubjectKey).
	SubjectKey value.Nullable[string] `foundry:"default=database"`
	// Payload binds Entry.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Entries.Payload, encoded) }.
	Payload value.JSON[json.RawMessage]
	// Redacted binds Entry.Redacted.
	// Example: orm.Set(Entries.Redacted, redacted).
	Redacted bool `foundry:"role=column"`
	// Redaction binds Entry.Redaction.
	// Example: orm.Set(Entries.Redaction, redaction).
	Redaction uint8
	// Origin binds Entry.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.Set(Entries.Origin, encoded) }.
	Origin value.JSON[attribution.Origin]
	// ActorKey binds Entry.ActorKey. Omission retains the database default.
	// Example: orm.SetNull(Entries.ActorKey).
	ActorKey value.Nullable[string] `foundry:"default=database"`
	// Correlation binds Entry.Correlation.
	// Example: orm.SetNull(Entries.Correlation).
	Correlation value.Nullable[string]
	// RequestMethod binds Entry.RequestMethod.
	// Example: orm.Set(Entries.RequestMethod, requestMethod).
	RequestMethod string
	// RequestRoute binds Entry.RequestRoute.
	// Example: orm.Set(Entries.RequestRoute, requestRoute).
	RequestRoute string
	// CreatedAt preserves an explicit creation time; omitted values use the database default.
	// Example: orm.Set(Entries.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"default=database,role=time"`
}

func (Entry) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("stored audit entry")) }

// Activity is the payload-free projection used by heterogeneous timelines.
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
