package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type channelCounters struct{ Incoming, Accepted, Completed, Failures, Published, Delivered, Replayed uint64 }
type ChannelSnapshot struct {
	ID                                                                      ChannelID
	Subscriptions                                                           int
	Incoming, Accepted, Completed, Failures, Published, Delivered, Replayed uint64
}
type Diagnostics struct {
	Runtime                                                         Snapshot
	QueuedFrames, QueuedBytes                                       int
	HeartbeatFailures, RateRejected, Revocations, ForcedDisconnects uint64
	Channels                                                        []ChannelSnapshot
}

// Diagnose requires both a typed authenticated guard and explicit management
// policy. No public HTTP endpoint is installed. Compose with the existing HTTP
// authentication adapter; its scope determines the administrative subject.
func Diagnose[M any](ctx context.Context, hub *Hub, guard auth.Guard[M], authorize func(context.Context, M) error) (Diagnostics, error) {
	if authorize == nil {
		return Diagnostics{}, fault.New(fault.Invalid, "WebSocket diagnostics require an authorization policy")
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return Diagnostics{}, err
	}
	defer finish()
	err = callback.Isolated("WebSocket diagnostics authorization", func() error {
		subject, err := guard.Require(ctx)
		if err != nil {
			return err
		}
		if err := authorize(ctx, subject); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return Diagnostics{}, err
	}
	return hub.diagnostics(), nil
}
func (h *Hub) diagnostics() Diagnostics {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := Diagnostics{Runtime: h.snapshotLocked(), HeartbeatFailures: h.heartbeatFailures, RateRejected: h.rateRejected, Revocations: h.revocations, ForcedDisconnects: h.forcedDisconnects, Channels: make([]ChannelSnapshot, 0, len(h.registry.ordered))}
	subscriptions := make(map[ChannelID]int, len(h.registry.channels))
	for _, connection := range h.connections {
		result.QueuedFrames += len(connection.outbound)
		result.QueuedBytes += connection.queuedBytes
		for _, pending := range connection.pending {
			result.QueuedFrames += len(pending.frames)
			result.QueuedBytes += pending.bytes
		}
		for key := range connection.subscriptions {
			subscriptions[key.channel]++
		}
	}
	for _, channel := range h.registry.ordered {
		metric := h.metrics[channel.id]
		result.Channels = append(result.Channels, ChannelSnapshot{ID: channel.id, Subscriptions: subscriptions[channel.id], Incoming: metric.Incoming, Accepted: metric.Accepted, Completed: metric.Completed, Failures: metric.Failures, Published: metric.Published, Delivered: metric.Delivered, Replayed: metric.Replayed})
	}
	return result
}
