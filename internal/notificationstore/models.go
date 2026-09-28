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
//foundry:model table=foundry_notifications
type Envelope struct {
	ID          model.ID[Envelope]
	Recipient   string
	Scope       string
	SubjectKey  string
	Identity    value.JSON[model.Identity]
	Origin      value.JSON[attribution.Origin]
	Name        string
	Version     uint32
	Payload     value.JSON[json.RawMessage]
	Fingerprint string
	CreatedAt   temporal.DateTime
}

// Delivery persists a channel independently. Claim remains stable from Running
// until completion; an abandoned Running row is never automatically resent.
//
//foundry:model table=foundry_notification_deliveries primary=Key
type Delivery struct {
	Key            string
	NotificationID model.ID[Envelope]
	Channel        string
	Kind           string
	State          string
	Payload        value.JSON[json.RawMessage]
	Claim          model.ID[Delivery]
	Attempts       uint32
	UpdatedAt      temporal.DateTime
}

// Inbox becomes visible atomically with database-channel delivery completion.
//
//foundry:model table=foundry_notification_inbox
type Inbox struct {
	ID         model.ID[Inbox]
	Scope      string
	SubjectKey string
	Name       string
	Version    uint32
	Data       value.JSON[json.RawMessage]
	CreatedAt  temporal.DateTime
	ReadAt     value.Nullable[temporal.DateTime]
}

func (Envelope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification envelope")) }
func (Delivery) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification delivery")) }
func (Inbox) Format(s fmt.State, _ rune)    { _, _ = s.Write([]byte("notification inbox row")) }
