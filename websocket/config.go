// Package websocket provides typed channels, room publication and the versioned
// Foundry WebSocket protocol. Applications own domain handlers and DTOs.
package websocket

import (
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

type Config struct {
	MaxPresenceScopes int
	MaxConnections    int
	MaxSubscriptions  int
	MaxFrameBytes     int
	// InboundQueue bounds frames read but not yet processed. Generated clients
	// limit their in-flight operations to it; overflowing it closes the socket.
	InboundQueue int
	// OutboundQueue bounds queued frames per connection. MaxQueuedBytes bounds
	// their actual bytes, including inbound frames and pending-admission
	// buffers; MaxTotalQueuedBytes bounds all connections together. Exceeding
	// either disconnects the slow connection instead of blocking publishers.
	OutboundQueue       int
	MaxQueuedBytes      int
	MaxTotalQueuedBytes int64
	// MaxOperations bounds concurrent trusted publication, presence and
	// disconnect operations. Callers wait briefly, then receive fault.Overloaded.
	MaxOperations            int
	MaxPresenceMembers       int
	MaxMemberBytes           int
	OperationTimeout         time.Duration
	WriteTimeout             time.Duration
	HeartbeatInterval        time.Duration
	PongTimeout              time.Duration
	AuthRefreshInterval      time.Duration
	DrainTimeout             time.Duration
	MaxConnectionsPerIP      int
	MaxConnectionsPerSubject int
	DeduplicationEntries     int
	MessageRate              ratelimit.Limit
	Payload                  contract.JSONLimits
	// AdditionalOrigins are exact HTTP(S) origins, never wildcard patterns.
	// Same-origin is accepted after trusted-proxy handling. Originless clients
	// must be explicitly allowed; serialized opaque/null origins never are.
	AdditionalOrigins []foundryhttp.Origin `config:",json"`
	AllowOriginless   bool
}

func DefaultConfig() Config {
	return Config{MaxPresenceScopes: 256, MaxConnections: 10000, MaxSubscriptions: 64, MaxFrameBytes: 64 << 10,
		InboundQueue: 64, OutboundQueue: 256, MaxQueuedBytes: 1 << 20, MaxTotalQueuedBytes: 256 << 20, MaxOperations: 1024,
		MaxPresenceMembers: 64, MaxMemberBytes: 512, OperationTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
		HeartbeatInterval: 15 * time.Second, PongTimeout: 5 * time.Second, AuthRefreshInterval: 30 * time.Second, DrainTimeout: time.Second,
		MaxConnectionsPerIP: 64, MaxConnectionsPerSubject: 8, DeduplicationEntries: 256, MessageRate: ratelimit.Limit{Requests: 128, Window: time.Second},
		Payload: contract.JSONLimits{Bytes: 32 << 10, Depth: 32, Nodes: 8192, Steps: 32768, Issues: 16}}
}

func (c Config) Validate() error {
	if c.MaxPresenceScopes < 1 || c.MaxPresenceScopes > 4096 || int64(c.MaxPresenceScopes)*int64(c.MaxPresenceMembers)*int64(c.MaxMemberBytes+128) > 128<<20 {
		return fault.New(fault.Invalid, "WebSocket presence scope budget exceeds its bound")
	}
	if c.HeartbeatInterval < 10*time.Millisecond || c.HeartbeatInterval > 5*time.Minute || c.PongTimeout < 10*time.Millisecond || c.PongTimeout > time.Minute || c.AuthRefreshInterval < 10*time.Millisecond || c.AuthRefreshInterval > 5*time.Minute || c.DrainTimeout < time.Millisecond || c.DrainTimeout > time.Minute || c.MaxConnectionsPerIP < 1 || c.MaxConnectionsPerIP > 65536 || c.MaxConnectionsPerSubject < 1 || c.MaxConnectionsPerSubject > 65536 || c.DeduplicationEntries < 1 || c.DeduplicationEntries > 65536 {
		return fault.New(fault.Invalid, "invalid WebSocket heartbeat, refresh, drain or connection bounds")
	}
	if err := c.MessageRate.Validate(); err != nil {
		return err
	}
	if c.MaxConnections < 1 || c.MaxConnections > 1<<20 || c.MaxSubscriptions < 1 || c.MaxSubscriptions > 1024 || c.MaxFrameBytes < 4096 || c.MaxFrameBytes > 1<<20 || c.InboundQueue < 1 || c.InboundQueue > 1024 || c.OutboundQueue < 1 || c.OutboundQueue > 4096 || c.MaxOperations < 1 || c.MaxOperations > 65536 || c.OperationTimeout <= 0 || c.OperationTimeout > time.Minute || c.WriteTimeout <= 0 || c.WriteTimeout > time.Minute {
		return fault.New(fault.Invalid, "invalid WebSocket runtime bounds")
	}
	// Queued bytes are enforced when frames are queued, so the budget is the
	// actual retained data rather than a worst-case per-connection product.
	if c.MaxQueuedBytes < c.MaxFrameBytes || c.MaxQueuedBytes > 64<<20 || c.MaxTotalQueuedBytes < int64(c.MaxQueuedBytes) || c.MaxTotalQueuedBytes > 16<<30 {
		return fault.New(fault.Invalid, "invalid WebSocket queued byte budget")
	}
	if err := c.Payload.Validate(); err != nil {
		return err
	}
	if c.Payload.Bytes > c.MaxFrameBytes-4096 || c.Payload.Depth > 63 || c.Payload.Nodes > 65500 || c.Payload.Steps > 1<<20 || c.Payload.Issues > 128 {
		return fault.New(fault.Invalid, "invalid WebSocket payload bounds")
	}
	if !validPresenceBounds(c.MaxPresenceMembers, c.MaxMemberBytes, c.MaxFrameBytes) || c.MaxMemberBytes > c.Payload.Bytes {
		return fault.New(fault.Invalid, "invalid WebSocket presence bounds")
	}
	if len(c.AdditionalOrigins) > 128 {
		return fault.New(fault.Invalid, "too many WebSocket origins")
	}
	seen := make(map[foundryhttp.Origin]bool)
	for _, raw := range c.AdditionalOrigins {
		origin, err := foundryhttp.ParseOrigin(string(raw))
		if err != nil || origin == foundryhttp.NullOrigin {
			return fault.New(fault.Invalid, "invalid WebSocket origin")
		}
		if seen[origin] {
			return fault.New(fault.Duplicate, "duplicate WebSocket origin")
		}
		seen[origin] = true
	}
	return nil
}
func (c Config) snapshot() Config { c.AdditionalOrigins = slices.Clone(c.AdditionalOrigins); return c }

func validPresenceBounds(members, memberBytes, frameBytes int) bool {
	return members >= 1 && members <= 4096 && memberBytes >= 2 && memberBytes <= 1<<20 && int64(members)*int64(memberBytes+256)+4096 <= int64(frameBytes)
}
