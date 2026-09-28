package websocket

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (c *connectionState) joinCluster(ctx context.Context, subscription *subscriptionState, data []byte, count int) ([]historyFrame, PresenceSnapshot, error) {
	h := c.hub
	state := h.cluster
	var presence PresenceSnapshot
	var history []historyFrame
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		var err error
		presence, err = state.backend.WebSocketJoin(ctx, state.key, state.instance, c.id, ClusterMembership{Scope: subscription.key.scope(), Subject: subscription.subjectID, Presence: subscription.member != "", Data: data})
		if err != nil {
			return err
		}
		if subscription.member != "" {
			presence, err = h.validateClusterPresence(ctx, subscription.key, presence)
			if err != nil {
				return err
			}
		}
		if count > 0 {
			frames, err := state.backend.WebSocketHistory(ctx, state.key, subscription.key.channel, subscription.channel.replay)
			if err != nil {
				return err
			}
			if len(frames) > subscription.channel.replay.Messages {
				return fault.New(fault.Invalid, "cluster replay exceeds its count bound")
			}
			bytes := 0
			for _, frame := range frames {
				bytes += len(frame)
				if bytes > subscription.channel.replay.Bytes {
					return fault.New(fault.Invalid, "cluster replay exceeds its byte bound")
				}
				frame = slices.Clone(frame)
				response, err := h.decodePublication(ctx, frame)
				if err != nil {
					return err
				}
				if response.Channel != subscription.key.channel {
					return fault.New(fault.Invalid, "cluster replay crossed channel boundaries")
				}
				if reaches(response, subscription.key) {
					history = append(history, historyFrame{response: response, data: frame})
				}
			}
			if len(history) > count {
				history = history[len(history)-count:]
			}
		}
		if subscription.member != "" {
			scope := subscription.key.scope()
			return h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPresenceChanged, Scope: &scope})
		}
		return nil
	})
	return history, presence, err
}
func (c *connectionState) leaveCluster(ctx context.Context, subscription *subscriptionState) error {
	if !subscription.clusterJoined {
		return nil
	}
	h := c.hub
	state := h.cluster
	return h.clusterCall(ctx, func(ctx context.Context) error {
		if err := state.backend.WebSocketLeave(ctx, state.key, state.instance, c.id, subscription.key.scope()); err != nil {
			return err
		}
		if subscription.member != "" {
			scope := subscription.key.scope()
			return h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPresenceChanged, Scope: &scope})
		}
		return nil
	})
}
func (c *connectionState) maintainCluster() error {
	h := c.hub
	state := h.cluster
	ctx, cancel := context.WithTimeout(c.ctx, state.config.ConnectionTTL/3)
	defer cancel()
	if err := h.clusterCall(ctx, func(ctx context.Context) error {
		return state.backend.WebSocketTouch(ctx, state.key, state.instance, c.id)
	}); err != nil {
		return err
	}
	h.mu.Lock()
	scopes := make([]Scope, 0, len(c.subscriptions))
	for key, subscription := range c.subscriptions {
		if subscription.member == "" {
			continue
		}
		owner := true
		for id, other := range h.connections {
			if other != c && other.subscriptions[key] != nil && id.String() < c.id.String() {
				owner = false
				break
			}
		}
		if owner {
			scopes = append(scopes, key.scope())
		}
	}
	h.mu.Unlock()
	for _, scope := range scopes {
		if err := h.refreshClusterPresence(ctx, scope); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Disconnect releases every remote membership in one atomic operation before
// per-channel domain cleanup. Abrupt/uncertain failures are covered by lease TTL.
func (c *connectionState) closeCluster(subscriptions []*subscriptionState) {
	if !c.clusterOpened {
		return
	}
	c.clusterOpened = false
	h := c.hub
	state := h.cluster
	_ = h.clusterCall(context.Background(), func(ctx context.Context) error {
		if err := state.backend.WebSocketClose(ctx, state.key, state.instance, c.id); err != nil {
			return err
		}
		for _, subscription := range subscriptions {
			if subscription.member != "" {
				scope := subscription.key.scope()
				if err := h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPresenceChanged, Scope: &scope}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
