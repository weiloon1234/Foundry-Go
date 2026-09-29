package websocket

import (
	"context"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type channelCounters struct {
	incoming, accepted, completed, failures, published, delivered, replayed atomic.Uint64
}
type ChannelSnapshot struct {
	ID                                                                      ChannelID
	Subscriptions                                                           int
	Incoming, Accepted, Completed, Failures, Published, Delivered, Replayed uint64
}
type Diagnostics struct {
	Runtime                                                         Snapshot
	QueuedFrames, QueuedBytes                                       int
	HeartbeatFailures, RateRejected, Revocations, ForcedDisconnects uint64
	// CleanupFailures counts cluster membership releases or stream closes that
	// failed; leases expire by TTL and presence is reconciled.
	CleanupFailures uint64
	Channels        []ChannelSnapshot
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
	result := Diagnostics{Runtime: h.snapshotLocked(), Channels: make([]ChannelSnapshot, 0, len(h.registry.ordered))}
	subscriptions := make(map[ChannelID]int, len(h.registry.channels))
	for key, connections := range h.subscribers {
		subscriptions[key.channel] += len(connections)
	}
	for _, connection := range h.connections {
		result.QueuedFrames += len(connection.outbound)
		for _, pending := range connection.pending {
			result.QueuedFrames += len(pending.frames)
		}
	}
	h.mu.Unlock()
	result.QueuedBytes = int(h.queuedBytes.Load())
	result.HeartbeatFailures, result.RateRejected = h.counters.heartbeatFailures.Load(), h.counters.rateRejected.Load()
	result.Revocations, result.ForcedDisconnects = h.counters.revocations.Load(), h.counters.forcedDisconnects.Load()
	result.CleanupFailures = h.counters.cleanupFailures.Load()
	for _, channel := range h.registry.ordered {
		metric := h.metrics[channel.id]
		result.Channels = append(result.Channels, ChannelSnapshot{ID: channel.id, Subscriptions: subscriptions[channel.id], Incoming: metric.incoming.Load(), Accepted: metric.accepted.Load(), Completed: metric.completed.Load(), Failures: metric.failures.Load(), Published: metric.published.Load(), Delivered: metric.delivered.Load(), Replayed: metric.replayed.Load()})
	}
	return result
}
