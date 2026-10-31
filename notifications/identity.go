// Package notifications composes typed recipients, payloads and delivery
// channels around persistent per-channel status. It borrows existing providers,
// database pools, mailers, WebSocket publishers and the jobs/outbox system.
package notifications

import "time"

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Name identifies a notification definition.
type Name string

// Version identifies a notification definition version.
type Version uint32

// RecipientName identifies a notification recipient declaration.
type RecipientName string

// ChannelID identifies a notification channel.
type ChannelID string

// ID is a notification identifier tied to its recipient type.
type ID[M any] = model.ID[NotificationOf[M]]

// NotificationOf marks notification identities belonging to a recipient type.
type NotificationOf[M any] struct{ _ [0]*M }

// NotificationID is the heterogeneous transport identity, not inbox authority.
type NotificationID = model.ID[Notification]

// Notification marks an untyped notification identity.
type Notification struct{}

// NewID creates a recipient-typed notification identifier using the current time.
func NewID[M any]() (ID[M], error) { return model.NewIDAt[NotificationOf[M]](time.Now()) }

// ParseID parses a recipient-typed notification identifier.
func ParseID[M any](text string) (ID[M], error) { return model.ParseID[NotificationOf[M]](text) }

// DeliveryID is a stable external idempotency key, derived from notification and
// channel identities. It is not recipient authentication or a provider receipt.
type DeliveryID struct{ key string }

// String returns the delivery key.
func (id DeliveryID) String() string { return id.key }

// Validate checks that the delivery key is a 64-character hexadecimal string.
func (id DeliveryID) Validate() error {
	if len(id.key) != 64 {
		return invalid()
	}
	_, err := hex.DecodeString(id.key)
	if err != nil {
		return invalid()
	}
	return nil
}

// Format writes a fixed label without exposing the delivery identifier.
func (DeliveryID) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("notification delivery identity"))
}
func digest(parts ...string) string {
	data, _ := json.Marshal(parts) // only owned strings; no extension callbacks
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
func invalid() error {
	return safeerror.NewFault(fault.Invalid, "invalid notification declaration or data")
}
func semantic(text string) bool            { return identifier.Semantic(text) }
func stringVersion(version Version) string { return strconv.FormatUint(uint64(version), 10) }

const MaxChannels = 16
const MaxPayloadBytes = 768 << 10

func payloadLimits() contract.JSONLimits {
	return contract.JSONLimits{Bytes: MaxPayloadBytes, Depth: 64, Nodes: 100000, Steps: 200000, Issues: 16}
}

// State is persisted independently for each channel. Running may mean an active
// call or a process lost after claiming; it is never automatically reclaimed.
type State string

const (
	Pending    State = "pending"
	Prepared   State = "prepared"
	Running    State = "running"
	Delivered  State = "delivered"
	Skipped    State = "skipped"
	Ineligible State = "ineligible"
	Rejected   State = "rejected"
	Uncertain  State = "uncertain"
)

// Retryable reports whether the state is pending or prepared.
func (s State) Retryable() bool { return s == Pending || s == Prepared }
func (s State) valid() bool {
	switch s {
	case Pending, Prepared, Running, Delivered, Skipped, Ineligible, Rejected, Uncertain:
		return true
	}
	return false
}

// ChannelStatus reports a channel's state and attempt count.
type ChannelStatus struct {
	Channel  ChannelID
	State    State
	Attempts uint32
}

// Report contains a notification identifier and channel delivery statuses.
type Report[M any] struct {
	ID       ID[M]
	Channels []ChannelStatus
}

// Retryable reports whether any channel is pending or prepared.
func (r Report[M]) Retryable() bool {
	for _, c := range r.Channels {
		if c.State.Retryable() {
			return true
		}
	}
	return false
}

// Format writes a fixed label without exposing notification results.
func (Report[M]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification report")) }

// Outcome is returned by custom transports. Retry means definite nonacceptance;
// never use it after a timeout that may have reached the remote service.
type Outcome uint8

const (
	Accepted Outcome = iota + 1
	Retry
	Reject
	Unknown
)
