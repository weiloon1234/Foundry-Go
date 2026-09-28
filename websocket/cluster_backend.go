package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

type Instance struct{}
type InstanceID = model.ID[Instance]

// Scope distinguishes whole-channel membership from an exact room. Adapters use
// its opaque Hash; applications retain Channel/Target rather than constructing it.
type Scope struct {
	Channel ChannelID `json:"channel"`
	Room    string    `json:"room"`
	HasRoom bool      `json:"has_room"`
}

func (s Scope) Validate() error {
	if !identifier.Semantic(string(s.Channel)) || s.HasRoom && !validRoom(s.Room) || !s.HasRoom && s.Room != "" {
		return fault.New(fault.Invalid, "invalid WebSocket scope")
	}
	return nil
}
func (s Scope) Hash() string {
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (k subscriptionKey) scope() Scope {
	return Scope{Channel: k.channel, Room: k.room, HasRoom: k.hasRoom}
}
func (s Scope) key() subscriptionKey {
	return subscriptionKey{channel: s.Channel, room: s.Room, hasRoom: s.HasRoom}
}

// ClusterLimits are immutable authority policy, shared by every process in one
// namespace. Metadata lives as long as connections or replay can remain live.
type ClusterLimits struct {
	Connections           int           `json:"connections"`
	ConnectionsPerSubject int           `json:"connections_per_subject"`
	Subscriptions         int           `json:"subscriptions"`
	PresenceMembers       int           `json:"presence_members"`
	MemberBytes           int           `json:"member_bytes"`
	FrameBytes            int           `json:"frame_bytes"`
	ConnectionTTL         time.Duration `json:"connection_ttl"`
	Retention             time.Duration `json:"retention"`
}

func (l ClusterLimits) Validate() error {
	if !validPresenceBounds(l.PresenceMembers, l.MemberBytes, l.FrameBytes) {
		return fault.New(fault.Invalid, "cluster presence cannot fit its bounded frame")
	}
	if l.Connections < 1 || l.Connections > 4096 || l.ConnectionsPerSubject < 1 || l.ConnectionsPerSubject > l.Connections || l.Subscriptions < 1 || l.Subscriptions > 1024 || l.PresenceMembers < 1 || l.PresenceMembers > 4096 || l.MemberBytes < 2 || l.MemberBytes > 1<<20 || l.FrameBytes < 4096 || l.FrameBytes > 1<<20 || l.ConnectionTTL < 100*time.Millisecond || l.ConnectionTTL > 5*time.Minute || l.ConnectionTTL%time.Millisecond != 0 || l.Retention < 2*l.ConnectionTTL || l.Retention > 24*time.Hour || l.Retention%time.Millisecond != 0 {
		return fault.New(fault.Invalid, "invalid WebSocket cluster authority bounds")
	}
	return nil
}

type ClusterKey struct {
	address keyaddress.Address
	policy  string
	limits  ClusterLimits
}

func NewClusterKey(namespace keyspace.Namespace, policy string, limits ClusterLimits) (ClusterKey, error) {
	address, err := keyaddress.New(namespace, "realtime", "cluster")
	if err != nil {
		return ClusterKey{}, err
	}
	key := ClusterKey{address, policy, limits}
	return key, key.Validate()
}
func (k ClusterKey) Validate() error {
	if err := k.address.Validate(); err != nil {
		return err
	}
	if !validDigest(k.policy) {
		return fault.New(fault.Invalid, "invalid WebSocket cluster policy fingerprint")
	}
	return k.limits.Validate()
}
func (k ClusterKey) String() string {
	if k.Validate() != nil {
		return ""
	}
	return k.address.String("websocket")
}
func (k ClusterKey) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k ClusterKey) Policy() string                { return k.policy }
func (k ClusterKey) Limits() ClusterLimits         { return k.limits }
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// ClusterMembership is an adapter boundary. Data contains only the channel's safe
// member DTO. Subject is an opaque guard/provider/stored-identity digest.
type ClusterMembership struct {
	Scope    Scope
	Subject  MemberID
	Presence bool
	Data     json.RawMessage
}

func (m ClusterMembership) Validate(limits ClusterLimits) error {
	if err := m.Scope.Validate(); err != nil {
		return err
	}
	if m.Subject != "" && !validDigest(string(m.Subject)) {
		return fault.New(fault.Invalid, "invalid WebSocket subject key")
	}
	if !m.Presence {
		if len(m.Data) != 0 {
			return fault.New(fault.Invalid, "non-presence membership has member data")
		}
		return nil
	}
	if m.Subject == "" {
		return fault.New(fault.Invalid, "presence membership needs a subject")
	}
	_, err := jsonwire.Decode(m.Data, jsonwire.Limits{Bytes: limits.MemberBytes, Depth: jsonwire.MaxDepth, Nodes: 65536})
	return err
}

// PresenceSnapshot carries authority order so concurrent refreshes cannot replace
// newer counts with an older result. Revisions are bounded positive Redis integers.
type PresenceSnapshot struct {
	Revision uint64        `json:"revision"`
	Members  []MemberFrame `json:"members"`
}

// ClusterBackend borrows the existing live pub/sub transport and adds bounded
// atomic lease/membership/history capabilities. Mutations are attempted once;
// errors may represent an unknown outcome. Expired connections never resurrect.
// Members excludes expired leases and counts each subject's distinct connections.
// Close/Leave are idempotent and never delete another instance's live ownership.
type ClusterBackend interface {
	pubsub.Backend
	WebSocketCheck(context.Context, ClusterKey) error
	WebSocketOpen(context.Context, ClusterKey, InstanceID, ConnectionID) error
	WebSocketTouch(context.Context, ClusterKey, InstanceID, ConnectionID) error
	WebSocketJoin(context.Context, ClusterKey, InstanceID, ConnectionID, ClusterMembership) (PresenceSnapshot, error)
	WebSocketLeave(context.Context, ClusterKey, InstanceID, ConnectionID, Scope) error
	WebSocketClose(context.Context, ClusterKey, InstanceID, ConnectionID) error
	WebSocketMembers(context.Context, ClusterKey, Scope) (PresenceSnapshot, error)
	WebSocketAppend(context.Context, ClusterKey, ChannelID, ReplayConfig, []byte) error
	WebSocketHistory(context.Context, ClusterKey, ChannelID, ReplayConfig) ([][]byte, error)
}

func ValidateClusterOperation(ctx context.Context, key ClusterKey) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "WebSocket cluster operation requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return key.Validate()
}
func ValidateClusterConnection(ctx context.Context, key ClusterKey, instance InstanceID, connection ConnectionID) error {
	if err := ValidateClusterOperation(ctx, key); err != nil {
		return err
	}
	if instance.IsZero() || connection.IsZero() {
		return fault.New(fault.Invalid, "WebSocket cluster operation needs instance and connection identities")
	}
	return nil
}
