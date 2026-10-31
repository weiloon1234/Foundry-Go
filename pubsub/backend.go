// Package pubsub provides typed, ephemeral publication with explicit loss and ownership.
package pubsub

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Name identifies a pub/sub topic family.
type Name string

// Version identifies a payload schema. Zero is invalid.
type Version uint32

// Channel is an opaque, versioned adapter address. Redis database numbers do not
// isolate pub/sub; application/environment identity is always part of this address.
type Channel struct {
	address keyaddress.Address
	version Version
}

// NewChannel constructs a namespaced, versioned adapter address from a logical resource key.
func NewChannel(namespace keyspace.Namespace, name Name, version Version, logical string) (Channel, error) {
	if version == 0 {
		return Channel{}, safeerror.NewFault(fault.Invalid, "pub/sub schema version must be positive")
	}
	address, err := keyaddress.New(namespace, string(name), logical)
	return Channel{address, version}, err
}

// Validate checks the address and positive schema version.
func (c Channel) Validate() error {
	if c.version == 0 {
		return safeerror.NewFault(fault.Invalid, "pub/sub channel is not initialized")
	}
	return c.address.Validate()
}

// Namespace returns the channel's application and environment scope.
func (c Channel) Namespace() keyspace.Namespace { return c.address.Namespace }

// String returns the physical versioned address, or an empty string for an invalid channel.
func (c Channel) String() string {
	if c.Validate() != nil {
		return ""
	}
	return c.address.String(ChannelFeature) + ":" + strconv.FormatUint(uint64(c.version), 10)
}

// Limits bound a subscription's payloads and pending messages. Queue bytes count
// payload bytes; fixed-size message metadata is additionally bounded by Messages.
type Limits struct{ Messages, Bytes, PayloadBytes, Channels int }

// DefaultLimits returns the default per-subscription buffer and channel bounds.
func DefaultLimits() Limits {
	return Limits{Messages: 64, Bytes: 4 << 20, PayloadBytes: 64 << 10, Channels: 64}
}

// Validate checks buffer sizes, payload size, and channel count against transport bounds.
func (l Limits) Validate() error {
	if l.Messages <= 0 || l.Messages > 65536 || l.Bytes <= 0 || l.Bytes > 64<<20 || l.PayloadBytes <= 0 || l.PayloadBytes > value.JSONMaxBytes || l.Bytes < l.PayloadBytes || l.Channels <= 0 || l.Channels > 1024 {
		return safeerror.NewFault(fault.Invalid, "invalid pub/sub buffer limits")
	}
	return nil
}

// Message owns its payload. Adapter streams may multiplex exact channels; the
// typed application API retains its concrete topic and resource key boundary.
type Message struct {
	Channel Channel
	Data    []byte
}

// Stream is ready when Subscribe returns. Next cancellation stops that wait;
// Close owns the subscription lifetime and must unblock pending reads. Done closes
// only after adapter work exits. Errors never imply an uninterrupted subscription.
// Implementations must not silently drop messages or hide reconnection gaps.
type Stream interface {
	// Next waits for the next owned message. Cancellation ends this wait without closing the stream.
	Next(context.Context) (Message, error)
	// Close ends the subscription and unblocks reads; ctx bounds the shutdown wait.
	Close(context.Context) error
	// Done closes after the stream's adapter work exits.
	Done() <-chan struct{}
	// Err returns the terminal subscription error.
	Err() error
}

// Backend exposes ephemeral fan-out, separate from jobs or durable events. Publish
// returns the authority's subscriber count, not a processing acknowledgement:
// memory counts subscriptions, Redis counts subscribed connections (one client
// multiplexes all of its subscriptions over one connection).
// Subscribe's context bounds establishment only; the returned stream is explicitly
// owned until Close or adapter shutdown. No implicit publish retry or fallback.
type Backend interface {
	// Publish sends data to channel and returns the authority's subscriber count, without confirming processing.
	Publish(context.Context, Channel, []byte) (uint64, error)
	// Subscribe establishes a ready stream over the exact channels using limits to bound buffering.
	Subscribe(context.Context, []Channel, Limits) (Stream, error)
}

var ErrClosed = safeerror.Sentinel(fault.Closed, "pub/sub subscription closed")
var ErrOverflow = safeerror.Sentinel(fault.Conflict, "pub/sub subscription lost messages because its buffer filled")
var ErrDisconnected = safeerror.Sentinel(fault.Conflict, "pub/sub subscription disconnected; delivery may have been lost")

// ValidatePublish checks cancellation, channel validity, and nonempty transport payload bounds.
func ValidatePublish(ctx context.Context, channel Channel, data []byte) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "pub/sub operation needs context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := channel.Validate(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > value.JSONMaxBytes {
		return safeerror.NewFault(fault.Invalid, "pub/sub payload exceeds transport bounds")
	}
	return nil
}

// ValidateSubscribe checks cancellation, limits, channel validity, and duplicate channels.
func ValidateSubscribe(ctx context.Context, channels []Channel, limits Limits) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "pub/sub subscription needs context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(channels) == 0 || len(channels) > limits.Channels {
		return safeerror.NewFault(fault.Invalid, "pub/sub channel count exceeds its bound")
	}
	seen := make(map[Channel]bool, len(channels))
	for _, channel := range channels {
		if err := channel.Validate(); err != nil {
			return err
		}
		if seen[channel] {
			return safeerror.NewFault(fault.Duplicate, "pub/sub subscription repeats a channel")
		}
		seen[channel] = true
	}
	return nil
}

// ChannelFeature is the physical address family owned by this adapter.
const ChannelFeature keyspace.Feature = "pubsub"
