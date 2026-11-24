// Package notificationstore owns private notification persistence models.
// Applications use notifications' typed descriptors and recipient-scoped inbox.
package notificationstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Envelope is a captured request, not a public inbox DTO.
//
//foundry:model table=foundry_notifications var=Envelopes key=ID identity=false
type Envelope struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Envelopes.Query()).
	//
	// ID binds Envelope.ID.
	// Example: orm.Set(Envelopes.ID, id).
	ID model.ID[Envelope] `foundry:"member=ID"`
	// Recipient binds Envelope.Recipient.
	// Example: orm.Set(Envelopes.Recipient, recipient).
	Recipient string
	// Scope binds Envelope.Scope.
	// Example: orm.Set(Envelopes.Scope, scope).
	Scope string
	// SubjectKey binds Envelope.SubjectKey.
	// Example: orm.Set(Envelopes.SubjectKey, subjectKey).
	SubjectKey string
	// Identity binds Envelope.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Envelopes.Identity, encoded) }.
	Identity value.JSON[model.Identity]
	// Origin binds Envelope.Origin.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.Set(Envelopes.Origin, encoded) }.
	Origin value.JSON[attribution.Origin]
	// Name binds Envelope.Name.
	// Example: orm.Set(Envelopes.Name, name).
	Name string
	// Version binds Envelope.Version.
	// Example: orm.Set(Envelopes.Version, version).
	Version uint32
	// Payload binds Envelope.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Envelopes.Payload, encoded) }.
	Payload value.JSON[json.RawMessage]
	// Fingerprint binds Envelope.Fingerprint.
	// Example: orm.Set(Envelopes.Fingerprint, fingerprint).
	Fingerprint string
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Envelopes.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
}

// Delivery persists a channel independently. Claim remains stable from Running
// until completion; an abandoned Running row is never automatically resent.
//
//foundry:model table=foundry_notification_deliveries var=Deliveries key=Key identity=false
type Delivery struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Deliveries.Query()).
	//
	// KeyColumn binds Delivery.Key.
	// Example: orm.Set(Deliveries.KeyColumn, key).
	Key string `foundry:"member=KeyColumn"`
	// NotificationID binds Delivery.NotificationID.
	// Example: orm.Set(Deliveries.NotificationID, notificationID).
	NotificationID model.ID[Envelope] `foundry:"role=column"`
	// Channel binds Delivery.Channel.
	// Example: orm.Set(Deliveries.Channel, channel).
	Channel string
	// Kind binds Delivery.Kind.
	// Example: orm.Set(Deliveries.Kind, kind).
	Kind string
	// State binds Delivery.State.
	// Example: orm.Set(Deliveries.State, state).
	State string
	// Payload binds Delivery.Payload.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Deliveries.Payload, encoded) }.
	Payload value.JSON[json.RawMessage]
	// Claim binds Delivery.Claim.
	// Example: orm.Set(Deliveries.Claim, claim).
	Claim model.ID[Delivery] `foundry:"role=column"`
	// Attempts binds Delivery.Attempts.
	// Example: orm.Set(Deliveries.Attempts, attempts).
	Attempts uint32
	// UpdatedAt is caller-managed; the ORM does not refresh it.
	// Example: orm.Set(Deliveries.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=time"`
}

// Inbox becomes visible atomically with database-channel delivery completion.
//
//foundry:model table=foundry_notification_inbox var=Inboxes key=ID identity=false
type Inbox struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Inboxes.Query()).
	//
	// ID binds Inbox.ID.
	// Example: orm.Set(Inboxes.ID, id).
	ID model.ID[Inbox] `foundry:"member=ID"`
	// Scope binds Inbox.Scope.
	// Example: orm.Set(Inboxes.Scope, scope).
	Scope string
	// SubjectKey binds Inbox.SubjectKey.
	// Example: orm.Set(Inboxes.SubjectKey, subjectKey).
	SubjectKey string
	// Name binds Inbox.Name.
	// Example: orm.Set(Inboxes.Name, name).
	Name string
	// Version binds Inbox.Version.
	// Example: orm.Set(Inboxes.Version, version).
	Version uint32
	// Data binds Inbox.Data.
	// Example: encoded, err := value.NewJSON(data); if err == nil { _ = orm.Set(Inboxes.Data, encoded) }.
	Data value.JSON[json.RawMessage]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Inboxes.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
	// ReadAt binds Inbox.ReadAt.
	// Example: orm.SetNull(Inboxes.ReadAt).
	ReadAt value.Nullable[temporal.DateTime]
}

func (Envelope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification envelope")) }
func (Delivery) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification delivery")) }
func (Inbox) Format(s fmt.State, _ rune)    { _, _ = s.Write([]byte("notification inbox row")) }
