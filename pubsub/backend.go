// Package pubsub provides typed, ephemeral publication with explicit loss and ownership.
package pubsub

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Name string
type Version uint32

// Channel is an opaque, versioned adapter address. Redis database numbers do not
// isolate pub/sub; application/environment identity is always part of this address.
type Channel struct {
	address keyaddress.Address
	version Version
}

func NewChannel(namespace keyspace.Namespace, name Name, version Version, logical string) (Channel, error) {
	if version == 0 {
		return Channel{}, fault.New(fault.Invalid, "pub/sub schema version must be positive")
	}
	address, err := keyaddress.New(namespace, string(name), logical)
	return Channel{address, version}, err
}
func (c Channel) Validate() error {
	if c.version == 0 {
		return fault.New(fault.Invalid, "pub/sub channel is not initialized")
	}
	return c.address.Validate()
}
func (c Channel) Namespace() keyspace.Namespace { return c.address.Namespace }
func (c Channel) String() string {
	if c.Validate() != nil {
		return ""
	}
	return c.address.String("pubsub") + ":" + strconv.FormatUint(uint64(c.version), 10)
}

// Limits bound a subscription's payloads and pending messages. Queue bytes count
// payload bytes; fixed-size message metadata is additionally bounded by Messages.
type Limits struct{ Messages, Bytes, PayloadBytes, Channels int }

func DefaultLimits() Limits {
	return Limits{Messages: 64, Bytes: 4 << 20, PayloadBytes: 64 << 10, Channels: 64}
}
func (l Limits) Validate() error {
	if l.Messages <= 0 || l.Messages > 65536 || l.Bytes <= 0 || l.Bytes > 64<<20 || l.PayloadBytes <= 0 || l.PayloadBytes > value.JSONMaxBytes || l.Bytes < l.PayloadBytes || l.Channels <= 0 || l.Channels > 1024 {
		return fault.New(fault.Invalid, "invalid pub/sub buffer limits")
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
	Next(context.Context) (Message, error)
	Close(context.Context) error
	Done() <-chan struct{}
	Err() error
}

// Backend exposes ephemeral fan-out, separate from jobs or durable events. Publish
// returns the authority's subscriber count, not a processing acknowledgement.
// Subscribe's context bounds establishment only; the returned stream is explicitly
// owned until Close or adapter shutdown. No implicit publish retry or fallback.
type Backend interface {
	Publish(context.Context, Channel, []byte) (uint64, error)
	Subscribe(context.Context, []Channel, Limits) (Stream, error)
}

var ErrClosed = fault.New(fault.Closed, "pub/sub subscription closed")
var ErrOverflow = fault.New(fault.Conflict, "pub/sub subscription lost messages because its buffer filled")
var ErrDisconnected = fault.New(fault.Conflict, "pub/sub subscription disconnected; delivery may have been lost")

func ValidatePublish(ctx context.Context, channel Channel, data []byte) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "pub/sub operation needs context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := channel.Validate(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > value.JSONMaxBytes {
		return fault.New(fault.Invalid, "pub/sub payload exceeds transport bounds")
	}
	return nil
}
func ValidateSubscribe(ctx context.Context, channels []Channel, limits Limits) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "pub/sub subscription needs context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(channels) == 0 || len(channels) > limits.Channels {
		return fault.New(fault.Invalid, "pub/sub channel count exceeds its bound")
	}
	seen := make(map[Channel]bool, len(channels))
	for _, channel := range channels {
		if err := channel.Validate(); err != nil {
			return err
		}
		if seen[channel] {
			return fault.New(fault.Duplicate, "pub/sub subscription repeats a channel")
		}
		seen[channel] = true
	}
	return nil
}
