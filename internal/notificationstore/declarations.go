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
	// Table provides model queries and writes. Example: Envelopes.Query().First(ctx,tx).
	orm.Table[Envelope, model.ID[Envelope]]
	// ID binds Envelope.ID.
	// Example: Envelopes.ID.Set(id).
	ID orm.Key[Envelope, model.ID[Envelope]]
	// Recipient binds Envelope.Recipient.
	// Example: Envelopes.Recipient.Set(recipient).
	Recipient orm.Text[Envelope, string]
	// Scope binds Envelope.Scope.
	// Example: Envelopes.Scope.Set(scope).
	Scope orm.Text[Envelope, string]
	// SubjectKey binds Envelope.SubjectKey.
	// Example: Envelopes.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Envelope, string]
	// Identity binds Envelope.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Envelopes.Identity.Set(encoded) }.
	Identity orm.JSON[Envelope, model.Identity]
	// Origin binds Envelope.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = Envelopes.Origin.Set(encoded) }.
	Origin orm.JSON[Envelope, attribution.Origin]
	// Name binds Envelope.Name.
	// Example: Envelopes.Name.Set(name).
	Name orm.Text[Envelope, string]
	// Version binds Envelope.Version.
	// Example: Envelopes.Version.Set(version).
	Version orm.Number[Envelope, uint32]
	// Payload binds Envelope.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = Envelopes.Payload.Set(encoded) }.
	Payload orm.JSON[Envelope, json.RawMessage]
	// Fingerprint binds Envelope.Fingerprint.
	// Example: Envelopes.Fingerprint.Set(fingerprint).
	Fingerprint orm.Text[Envelope, string]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: Envelopes.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Envelope, temporal.DateTime]
}

// Envelopes owns the immutable Envelope declaration. Example: Envelopes.Query().First(ctx,tx).
var Envelopes = orm.Define("foundry_notifications", func(b orm.Builder[Envelope], m *Envelope) EnvelopeTable {
	table := orm.TableOf(b, &m.ID)
	return EnvelopeTable{Table: table, ID: table.Key(), Recipient: orm.TextOf(b, &m.Recipient), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Origin: orm.JSONOf(b, &m.Origin), Name: orm.TextOf(b, &m.Name), Version: orm.NumberOf(b, &m.Version), Payload: orm.JSONOf(b, &m.Payload), Fingerprint: orm.TextOf(b, &m.Fingerprint), CreatedAt: orm.TimeOf(b, &m.CreatedAt)}
})

// DeliveryTable binds the persisted Delivery columns. Example: Deliveries.Query().Limit(10).
type DeliveryTable struct {
	// Table provides model queries and writes. Example: Deliveries.Query().First(ctx,tx).
	orm.Table[Delivery, string]
	// KeyColumn binds Delivery.Key.
	// Example: Deliveries.KeyColumn.Set(key).
	KeyColumn orm.TextLookupKey[Delivery, string]
	// NotificationID binds Delivery.NotificationID.
	// Example: Deliveries.NotificationID.Set(notificationID).
	NotificationID orm.Column[Delivery, model.ID[Envelope]]
	// Channel binds Delivery.Channel.
	// Example: Deliveries.Channel.Set(channel).
	Channel orm.Text[Delivery, string]
	// Kind binds Delivery.Kind.
	// Example: Deliveries.Kind.Set(kind).
	Kind orm.Text[Delivery, string]
	// State binds Delivery.State.
	// Example: Deliveries.State.Set(state).
	State orm.Text[Delivery, string]
	// Payload binds Delivery.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = Deliveries.Payload.Set(encoded) }.
	Payload orm.JSON[Delivery, json.RawMessage]
	// Claim binds Delivery.Claim.
	// Example: Deliveries.Claim.Set(claim).
	Claim orm.Column[Delivery, model.ID[Delivery]]
	// Attempts binds Delivery.Attempts.
	// Example: Deliveries.Attempts.Set(attempts).
	Attempts orm.Number[Delivery, uint32]
	// UpdatedAt is caller-managed; the ORM does not refresh it.
	// Example: Deliveries.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.Time[Delivery, temporal.DateTime]
}

// Deliveries owns the immutable Delivery declaration. Example: Deliveries.Query().First(ctx,tx).
var Deliveries = orm.Define("foundry_notification_deliveries", func(b orm.Builder[Delivery], m *Delivery) DeliveryTable {
	table := orm.TableOf(b, &m.Key)
	return DeliveryTable{Table: table, KeyColumn: orm.TextKey(table.Key()), NotificationID: orm.ColumnOf(b, &m.NotificationID), Channel: orm.TextOf(b, &m.Channel), Kind: orm.TextOf(b, &m.Kind), State: orm.TextOf(b, &m.State), Payload: orm.JSONOf(b, &m.Payload), Claim: orm.ColumnOf(b, &m.Claim), Attempts: orm.NumberOf(b, &m.Attempts), UpdatedAt: orm.TimeOf(b, &m.UpdatedAt)}
})

// InboxTable binds the persisted Inbox columns. Example: Inboxes.Query().Limit(10).
type InboxTable struct {
	// Table provides model queries and writes. Example: Inboxes.Query().First(ctx,tx).
	orm.Table[Inbox, model.ID[Inbox]]
	// ID binds Inbox.ID.
	// Example: Inboxes.ID.Set(id).
	ID orm.Key[Inbox, model.ID[Inbox]]
	// Scope binds Inbox.Scope.
	// Example: Inboxes.Scope.Set(scope).
	Scope orm.Text[Inbox, string]
	// SubjectKey binds Inbox.SubjectKey.
	// Example: Inboxes.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[Inbox, string]
	// Name binds Inbox.Name.
	// Example: Inboxes.Name.Set(name).
	Name orm.Text[Inbox, string]
	// Version binds Inbox.Version.
	// Example: Inboxes.Version.Set(version).
	Version orm.Number[Inbox, uint32]
	// Data binds Inbox.Data.
	// Example: encoded, err := value.NewJSON(data); if err == nil { _ = Inboxes.Data.Set(encoded) }.
	Data orm.JSON[Inbox, json.RawMessage]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: Inboxes.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Inbox, temporal.DateTime]
	// ReadAt binds Inbox.ReadAt.
	// Example: Inboxes.ReadAt.SetNull().
	ReadAt orm.NullableTime[Inbox, temporal.DateTime]
}

// Inboxes owns the immutable Inbox declaration. Example: Inboxes.Query().First(ctx,tx).
var Inboxes = orm.Define("foundry_notification_inbox", func(b orm.Builder[Inbox], m *Inbox) InboxTable {
	table := orm.TableOf(b, &m.ID)
	return InboxTable{Table: table, ID: table.Key(), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Name: orm.TextOf(b, &m.Name), Version: orm.NumberOf(b, &m.Version), Data: orm.JSONOf(b, &m.Data), CreatedAt: orm.TimeOf(b, &m.CreatedAt), ReadAt: orm.NullableTimeOf(b, &m.ReadAt)}
})
