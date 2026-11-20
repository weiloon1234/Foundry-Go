package notificationstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// EnvelopeTable binds the persisted Envelope columns. Example: Envelopes.Query().Limit(10).
type EnvelopeTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Envelopes.Query()).
	orm.Table[Envelope, model.ID[Envelope]]
	// ID binds Envelope.ID.
	// Example: orm.Set(Envelopes.ID, id).
	ID orm.Key[Envelope, model.ID[Envelope]]
	// Recipient binds Envelope.Recipient.
	// Example: orm.Set(Envelopes.Recipient, recipient).
	Recipient orm.Text[Envelope, string]
	// Scope binds Envelope.Scope.
	// Example: orm.Set(Envelopes.Scope, scope).
	Scope orm.Text[Envelope, string]
	// SubjectKey binds Envelope.SubjectKey.
	// Example: orm.Set(Envelopes.SubjectKey, subjectKey).
	SubjectKey orm.Text[Envelope, string]
	// Identity binds Envelope.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Envelopes.Identity, encoded) }.
	Identity orm.JSON[Envelope, model.Identity]
	// Origin binds Envelope.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.Set(Envelopes.Origin, encoded) }.
	Origin orm.JSON[Envelope, attribution.Origin]
	// Name binds Envelope.Name.
	// Example: orm.Set(Envelopes.Name, name).
	Name orm.Text[Envelope, string]
	// Version binds Envelope.Version.
	// Example: orm.Set(Envelopes.Version, version).
	Version orm.Number[Envelope, uint32]
	// Payload binds Envelope.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Envelopes.Payload, encoded) }.
	Payload orm.JSON[Envelope, json.RawMessage]
	// Fingerprint binds Envelope.Fingerprint.
	// Example: orm.Set(Envelopes.Fingerprint, fingerprint).
	Fingerprint orm.Text[Envelope, string]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Envelopes.CreatedAt, createdAt).
	CreatedAt orm.Time[Envelope, temporal.DateTime]
}

// Envelopes owns the immutable Envelope declaration. Example: orm.First(ctx, tx, Envelopes.Query()).
var Envelopes = orm.Define("foundry_notifications", func(b orm.Builder[Envelope], m *Envelope) EnvelopeTable {
	table := orm.TableOf(b, &m.ID)
	return EnvelopeTable{Table: table, ID: table.Key(), Recipient: orm.TextOf(b, &m.Recipient), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Origin: orm.JSONOf(b, &m.Origin), Name: orm.TextOf(b, &m.Name), Version: orm.NumberOf(b, &m.Version), Payload: orm.JSONOf(b, &m.Payload), Fingerprint: orm.TextOf(b, &m.Fingerprint), CreatedAt: orm.TimeOf(b, &m.CreatedAt)}
})

// DeliveryTable binds the persisted Delivery columns. Example: Deliveries.Query().Limit(10).
type DeliveryTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Deliveries.Query()).
	orm.Table[Delivery, string]
	// KeyColumn binds Delivery.Key.
	// Example: orm.Set(Deliveries.KeyColumn, key).
	KeyColumn orm.TextLookupKey[Delivery, string]
	// NotificationID binds Delivery.NotificationID.
	// Example: orm.Set(Deliveries.NotificationID, notificationID).
	NotificationID orm.Column[Delivery, model.ID[Envelope]]
	// Channel binds Delivery.Channel.
	// Example: orm.Set(Deliveries.Channel, channel).
	Channel orm.Text[Delivery, string]
	// Kind binds Delivery.Kind.
	// Example: orm.Set(Deliveries.Kind, kind).
	Kind orm.Text[Delivery, string]
	// State binds Delivery.State.
	// Example: orm.Set(Deliveries.State, state).
	State orm.Text[Delivery, string]
	// Payload binds Delivery.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Deliveries.Payload, encoded) }.
	Payload orm.JSON[Delivery, json.RawMessage]
	// Claim binds Delivery.Claim.
	// Example: orm.Set(Deliveries.Claim, claim).
	Claim orm.Column[Delivery, model.ID[Delivery]]
	// Attempts binds Delivery.Attempts.
	// Example: orm.Set(Deliveries.Attempts, attempts).
	Attempts orm.Number[Delivery, uint32]
	// UpdatedAt is caller-managed; the ORM does not refresh it.
	// Example: orm.Set(Deliveries.UpdatedAt, updatedAt).
	UpdatedAt orm.Time[Delivery, temporal.DateTime]
}

// Deliveries owns the immutable Delivery declaration. Example: orm.First(ctx, tx, Deliveries.Query()).
var Deliveries = orm.Define("foundry_notification_deliveries", func(b orm.Builder[Delivery], m *Delivery) DeliveryTable {
	table := orm.TableOf(b, &m.Key)
	return DeliveryTable{Table: table, KeyColumn: orm.TextKey(table.Key()), NotificationID: orm.ColumnOf(b, &m.NotificationID), Channel: orm.TextOf(b, &m.Channel), Kind: orm.TextOf(b, &m.Kind), State: orm.TextOf(b, &m.State), Payload: orm.JSONOf(b, &m.Payload), Claim: orm.ColumnOf(b, &m.Claim), Attempts: orm.NumberOf(b, &m.Attempts), UpdatedAt: orm.TimeOf(b, &m.UpdatedAt)}
})

// InboxTable binds the persisted Inbox columns. Example: Inboxes.Query().Limit(10).
type InboxTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Inboxes.Query()).
	orm.Table[Inbox, model.ID[Inbox]]
	// ID binds Inbox.ID.
	// Example: orm.Set(Inboxes.ID, id).
	ID orm.Key[Inbox, model.ID[Inbox]]
	// Scope binds Inbox.Scope.
	// Example: orm.Set(Inboxes.Scope, scope).
	Scope orm.Text[Inbox, string]
	// SubjectKey binds Inbox.SubjectKey.
	// Example: orm.Set(Inboxes.SubjectKey, subjectKey).
	SubjectKey orm.Text[Inbox, string]
	// Name binds Inbox.Name.
	// Example: orm.Set(Inboxes.Name, name).
	Name orm.Text[Inbox, string]
	// Version binds Inbox.Version.
	// Example: orm.Set(Inboxes.Version, version).
	Version orm.Number[Inbox, uint32]
	// Data binds Inbox.Data.
	// Example: encoded, err := value.NewJSON(data); if err == nil { _ = orm.Set(Inboxes.Data, encoded) }.
	Data orm.JSON[Inbox, json.RawMessage]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Inboxes.CreatedAt, createdAt).
	CreatedAt orm.Time[Inbox, temporal.DateTime]
	// ReadAt binds Inbox.ReadAt.
	// Example: orm.SetNull(Inboxes.ReadAt).
	ReadAt orm.NullableTime[Inbox, temporal.DateTime]
}

// Inboxes owns the immutable Inbox declaration. Example: orm.First(ctx, tx, Inboxes.Query()).
var Inboxes = orm.Define("foundry_notification_inbox", func(b orm.Builder[Inbox], m *Inbox) InboxTable {
	table := orm.TableOf(b, &m.ID)
	return InboxTable{Table: table, ID: table.Key(), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Name: orm.TextOf(b, &m.Name), Version: orm.NumberOf(b, &m.Version), Data: orm.JSONOf(b, &m.Data), CreatedAt: orm.TimeOf(b, &m.CreatedAt), ReadAt: orm.NullableTimeOf(b, &m.ReadAt)}
})
