package auditstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// EntryTable binds the persisted Entry columns. Example: Entries.Query().Limit(10).
type EntryTable struct {
	// Table provides model queries and writes. Example: Entries.Query().First(ctx,tx).
	orm.Table[Entry, model.ID[Entry]]
	// ID binds Entry.ID.
	// Example: Entries.ID.Set(id).
	ID orm.Key[Entry, model.ID[Entry]]
	// Sequence binds Entry.Sequence. Omission retains the database default.
	// Example: Entries.Sequence.Set(sequence).
	Sequence orm.Number[Entry, int64]
	// Area binds Entry.Area.
	// Example: Entries.Area.Set(area).
	Area orm.Text[Entry, string]
	// Operation binds Entry.Operation.
	// Example: Entries.Operation.Set(operation).
	Operation orm.Column[Entry, lifecycle.Operation]
	// Action binds Entry.Action.
	// Example: Entries.Action.Set(action).
	Action orm.Text[Entry, string]
	// Version binds Entry.Version.
	// Example: Entries.Version.Set(version).
	Version orm.Number[Entry, uint32]
	// Subject binds Entry.Subject.
	// Example: Entries.Subject.SetNull().
	Subject orm.NullableJSON[Entry, model.Identity]
	// SubjectKey binds Entry.SubjectKey. Omission retains the database default.
	// Example: Entries.SubjectKey.SetNull().
	SubjectKey orm.NullableText[Entry, string]
	// Payload binds Entry.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = Entries.Payload.Set(encoded) }.
	Payload orm.JSON[Entry, json.RawMessage]
	// Redacted binds Entry.Redacted.
	// Example: Entries.Redacted.Set(redacted).
	Redacted orm.Column[Entry, bool]
	// Redaction binds Entry.Redaction.
	// Example: Entries.Redaction.Set(redaction).
	Redaction orm.Number[Entry, uint8]
	// Origin binds Entry.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = Entries.Origin.Set(encoded) }.
	Origin orm.JSON[Entry, attribution.Origin]
	// ActorKey binds Entry.ActorKey. Omission retains the database default.
	// Example: Entries.ActorKey.SetNull().
	ActorKey orm.NullableText[Entry, string]
	// Correlation binds Entry.Correlation.
	// Example: Entries.Correlation.SetNull().
	Correlation orm.NullableText[Entry, string]
	// RequestMethod binds Entry.RequestMethod.
	// Example: Entries.RequestMethod.Set(requestMethod).
	RequestMethod orm.Text[Entry, string]
	// RequestRoute binds Entry.RequestRoute.
	// Example: Entries.RequestRoute.Set(requestRoute).
	RequestRoute orm.Text[Entry, string]
	// CreatedAt preserves an explicit creation time; omitted values use the database default.
	// Example: Entries.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Entry, temporal.DateTime]
}

// Entries owns the immutable Entry declaration. Example: Entries.Query().First(ctx,tx).
var Entries = orm.Define("foundry_audit", func(b orm.Builder[Entry], m *Entry) EntryTable {
	table := orm.TableOf(b, &m.ID)
	return EntryTable{Table: table, ID: table.Key(), Sequence: orm.NumberOf(b, &m.Sequence), Area: orm.TextOf(b, &m.Area), Operation: orm.ColumnOf(b, &m.Operation), Action: orm.TextOf(b, &m.Action), Version: orm.NumberOf(b, &m.Version), Subject: orm.NullableJSONOf(b, &m.Subject), SubjectKey: orm.NullableTextOf(b, &m.SubjectKey), Payload: orm.JSONOf(b, &m.Payload), Redacted: orm.ColumnOf(b, &m.Redacted), Redaction: orm.NumberOf(b, &m.Redaction), Origin: orm.JSONOf(b, &m.Origin), ActorKey: orm.NullableTextOf(b, &m.ActorKey), Correlation: orm.NullableTextOf(b, &m.Correlation), RequestMethod: orm.TextOf(b, &m.RequestMethod), RequestRoute: orm.TextOf(b, &m.RequestRoute), CreatedAt: orm.TimeOf(b, &m.CreatedAt)}
})
