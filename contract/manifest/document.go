// Package manifest assembles feature-owned transport metadata into one
// versioned client contract. OpenAPI and language SDKs are output adapters.
package manifest

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// Version 4 adds required idempotency keys and transaction/replay policy metadata.
// Earlier formats must be regenerated; older readers reject unsupported versions.
const Version = 4
const MaxBytes = 16 << 20
const MaxOperations = 4096

// Sources contains actual registered descriptors. Schemas adds explicitly
// public DTOs with no operation; persistence models are never inspected.
type Sources struct {
	HTTP          *foundryhttp.Router
	Realtime      *websocket.ClientDescription
	Notifications *notifications.Registry
	Tables        *datatable.Registry
	Catalog       *i18n.Catalog
	Enums         []enum.Definition
	Permissions   []auth.PermissionDescription
	Schemas       []contract.Schema
}

// Document is the serialized inspection boundary. All payload references point
// into Types. Use Build or Decode to validate and freeze it before exporting.
type Document struct {
	Version       int                           `json:"version"`
	Types         []contract.Type               `json:"types"`
	Roots         []contract.TypeID             `json:"roots"`
	HTTP          []Operation                   `json:"http"`
	RawRoutes     []foundryhttp.RouteInfo       `json:"raw_routes,omitempty"`
	ErrorType     contract.TypeID               `json:"error_type"`
	Errors        []foundryhttp.ErrorDefinition `json:"errors"`
	Realtime      *Realtime                     `json:"realtime,omitempty"`
	Notifications []Notification                `json:"notifications,omitempty"`
	Tables        []Table                       `json:"tables,omitempty"`
	Locales       *Locales                      `json:"locales,omitempty"`
	Enums         []enum.Definition             `json:"enums,omitempty"`
	Permissions   []auth.PermissionDescription  `json:"permissions,omitempty"`
}

type Operation struct {
	Route             foundryhttp.RouteInfo        `json:"route"`
	Name              string                       `json:"name"`
	Path              []Parameter                  `json:"path"`
	Query             []Parameter                  `json:"query"`
	Body              *Payload                     `json:"body,omitempty"`
	Response          *Payload                     `json:"response,omitempty"`
	Status            int                          `json:"status"`
	FileTransferBytes int64                        `json:"file_transfer_bytes,omitempty"`
	Limits            foundryhttp.EndpointLimits   `json:"limits"`
	Idempotency       *foundryhttp.IdempotencyInfo `json:"idempotency,omitempty"`
	Preparation       bool                         `json:"preparation,omitempty"`
	Validation        *validation.Description      `json:"validation,omitempty"`
	Errors            []foundryhttp.ErrorCode      `json:"errors,omitempty"`
}

type Parameter struct {
	Name       string                 `json:"name"`
	Type       contract.TypeID        `json:"type,omitempty"`
	Syntax     foundryhttp.URLSyntax  `json:"syntax,omitempty"`
	Required   bool                   `json:"required"`
	Repeated   bool                   `json:"repeated"`
	CatchAll   bool                   `json:"catch_all,omitempty"`
	DefaultURL value.Optional[string] `json:"default_url,omitzero"`
}

type Part struct {
	Parameter
	Kind foundryhttp.MultipartKind `json:"kind"`
}

type Payload struct {
	MediaType string                        `json:"media_type,omitempty"`
	Type      contract.TypeID               `json:"type,omitempty"`
	Fields    []Parameter                   `json:"fields,omitempty"`
	Parts     []Part                        `json:"parts,omitempty"`
	File      *foundryhttp.FileResponseInfo `json:"file,omitempty"`
}

type Realtime struct {
	Protocol websocket.ProtocolInfo `json:"protocol"`
	Limits   websocket.ClientLimits `json:"limits"`
	Channels []Channel              `json:"channels"`
}

type Channel struct {
	ID         websocket.ChannelID    `json:"id"`
	Name       string                 `json:"name"`
	Room       Parameter              `json:"room"`
	Private    bool                   `json:"private"`
	OwnedRooms bool                   `json:"owned_rooms"`
	Guard      auth.GuardName         `json:"guard,omitempty"`
	Provider   auth.ProviderName      `json:"provider,omitempty"`
	Presence   contract.TypeID        `json:"presence,omitempty"`
	Replay     websocket.ReplayConfig `json:"replay"`
	Events     []Event                `json:"events"`
}

type Event struct {
	ID                      websocket.EventID   `json:"id"`
	Name                    string              `json:"name"`
	Direction               websocket.Direction `json:"direction"`
	Dynamic                 bool                `json:"dynamic"`
	AcceptedAcknowledgement bool                `json:"accepted_acknowledgement"`
	Payload                 contract.TypeID     `json:"payload"`
}

type Notification struct {
	Recipient notifications.RecipientName `json:"recipient"`
	Name      notifications.Name          `json:"name"`
	Version   notifications.Version       `json:"version"`
	Channels  []NotificationChannel       `json:"channels"`
}
type NotificationChannel struct {
	ID       notifications.ChannelID           `json:"id"`
	Kind     string                            `json:"kind"`
	Payload  contract.TypeID                   `json:"payload"`
	Realtime *notifications.ClientRealtimeInfo `json:"realtime,omitempty"`
}

type Table struct {
	ID          datatable.TableID           `json:"id"`
	Row         contract.TypeID             `json:"row"`
	Request     contract.TypeID             `json:"request"`
	Columns     []datatable.ColumnInfo      `json:"columns"`
	Filters     []datatable.NamedFilterInfo `json:"filters,omitempty"`
	DefaultSort []datatable.Sort            `json:"default_sort,omitempty"`
	Exports     bool                        `json:"exports"`
}

type Locales struct {
	Default   i18n.LocaleID            `json:"default"`
	Supported []i18n.LocaleID          `json:"supported"`
	Messages  []i18n.MessageDefinition `json:"messages"`
}

// Manifest owns its canonical serialized document. Neither source mutation nor
// mutation of a Snapshot can change another exporter or a concurrent reader.
type Manifest struct{ data []byte }
