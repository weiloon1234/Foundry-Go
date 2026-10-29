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
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Entries.Query()).
	orm.Table[Entry, model.ID[Entry]]
	// ID binds Entry.ID.
	// Example: orm.KeySet(Entries.ID, id).
	ID orm.Key[Entry, model.ID[Entry]]
	// Sequence binds Entry.Sequence. Omission retains the database default.
	// Example: orm.NumberSet(Entries.Sequence, sequence).
	Sequence orm.Number[Entry, int64]
	// Area binds Entry.Area.
	// Example: orm.TextSet(Entries.Area, area).
	Area orm.Text[Entry, string]
	// Operation binds Entry.Operation.
	// Example: orm.ColumnSet(Entries.Operation, operation).
	Operation orm.Column[Entry, lifecycle.Operation]
	// Action binds Entry.Action.
	// Example: orm.TextSet(Entries.Action, action).
	Action orm.Text[Entry, string]
	// Version binds Entry.Version.
	// Example: orm.NumberSet(Entries.Version, version).
	Version orm.Number[Entry, uint32]
	// Subject binds Entry.Subject.
	// Example: orm.NullableJSONSetNull(Entries.Subject).
	Subject orm.NullableJSON[Entry, model.Identity]
	// SubjectKey binds Entry.SubjectKey. Omission retains the database default.
	// Example: orm.NullableTextSetNull(Entries.SubjectKey).
	SubjectKey orm.NullableText[Entry, string]
	// Payload binds Entry.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.JSONSet(Entries.Payload, encoded) }.
	Payload orm.JSON[Entry, json.RawMessage]
	// Redacted binds Entry.Redacted.
	// Example: orm.ColumnSet(Entries.Redacted, redacted).
	Redacted orm.Column[Entry, bool]
	// Redaction binds Entry.Redaction.
	// Example: orm.NumberSet(Entries.Redaction, redaction).
	Redaction orm.Number[Entry, uint8]
	// Origin binds Entry.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.JSONSet(Entries.Origin, encoded) }.
	Origin orm.JSON[Entry, attribution.Origin]
	// ActorKey binds Entry.ActorKey. Omission retains the database default.
	// Example: orm.NullableTextSetNull(Entries.ActorKey).
	ActorKey orm.NullableText[Entry, string]
	// Correlation binds Entry.Correlation.
	// Example: orm.NullableTextSetNull(Entries.Correlation).
	Correlation orm.NullableText[Entry, string]
	// RequestMethod binds Entry.RequestMethod.
	// Example: orm.TextSet(Entries.RequestMethod, requestMethod).
	RequestMethod orm.Text[Entry, string]
	// RequestRoute binds Entry.RequestRoute.
	// Example: orm.TextSet(Entries.RequestRoute, requestRoute).
	RequestRoute orm.Text[Entry, string]
	// CreatedAt preserves an explicit creation time; omitted values use the database default.
	// Example: orm.TimeSet(Entries.CreatedAt, createdAt).
	CreatedAt orm.Time[Entry, temporal.DateTime]
}

// Entries owns the immutable Entry declaration. Example: orm.First(ctx, tx, Entries.Query()).
var Entries = orm.Define("foundry_audit", func(b orm.Builder[Entry], m *Entry) EntryTable {
	table := orm.TableOf(b, &m.ID)
	return EntryTable{Table: table, ID: table.Key(), Sequence: orm.NumberOf(b, &m.Sequence), Area: orm.TextOf(b, &m.Area), Operation: orm.ColumnOf(b, &m.Operation), Action: orm.TextOf(b, &m.Action), Version: orm.NumberOf(b, &m.Version), Subject: orm.NullableJSONOf(b, &m.Subject), SubjectKey: orm.NullableTextOf(b, &m.SubjectKey), Payload: orm.JSONOf(b, &m.Payload), Redacted: orm.ColumnOf(b, &m.Redacted), Redaction: orm.NumberOf(b, &m.Redaction), Origin: orm.JSONOf(b, &m.Origin), ActorKey: orm.NullableTextOf(b, &m.ActorKey), Correlation: orm.NullableTextOf(b, &m.Correlation), RequestMethod: orm.TextOf(b, &m.RequestMethod), RequestRoute: orm.TextOf(b, &m.RequestRoute), CreatedAt: orm.TimeOf(b, &m.CreatedAt)}
})
