package websocket

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

func (c *connectionState) joinCluster(ctx context.Context, subscription *subscriptionState, data []byte, count int) ([]historyFrame, PresenceSnapshot, error) {
	h := c.hub
	state := h.cluster
	var presence PresenceSnapshot
	var history []historyFrame
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		membership := ClusterMembership{Scope: subscription.key.scope(), Subject: subscription.subjectID, Presence: subscription.member != "", Data: data}
		var err error
		presence, err = state.backend.WebSocketJoin(ctx, state.key, state.instance, c.id, membership)
		replaced := false
		if err != nil && isMembershipConflict(err) {
			// A leave that failed transiently left this connection's previous
			// record for the scope; release it and join with the new one.
			if err := state.backend.WebSocketLeave(ctx, state.key, state.instance, c.id, membership.Scope); err != nil {
				return err
			}
			replaced = true
			presence, err = state.backend.WebSocketJoin(ctx, state.key, state.instance, c.id, membership)
		}
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
		if subscription.member != "" || replaced {
			scope := subscription.key.scope()
			return h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPresenceChanged, Scope: &scope})
		}
		return nil
	})
	return history, presence, err
}

// isMembershipConflict inspects an adapter error with the bounded walker; it
// runs inside clusterCall's callback isolation.
func isMembershipConflict(err error) bool {
	found := false
	errorgraph.Walk(err, func(current error) bool {
		found = errorgraph.Matches(current, MembershipConflict)
		return !found
	})
	return found
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

// touchCluster renews this connection's lease. A transient authority failure is
// tolerated until the last renewed lease would expire; lost ownership (expired or
// reassigned record) or a terminal fault disconnects the connection.
func (c *connectionState) touchCluster(now time.Time) bool {
	h := c.hub
	state := h.cluster
	// A renewal ends before the next one is due; its own deadline is a
	// transient authority delay like any other, never a lost lease.
	ctx, cancel := context.WithTimeout(c.ctx, min(state.config.ConnectionTTL/3, state.config.OperationTimeout))
	defer cancel()
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		return state.backend.WebSocketTouch(ctx, state.key, state.instance, c.id)
	})
	transient := clusterCode(err) == Unavailable
	if err != nil && !transient && c.ctx.Err() == nil && ctx.Err() != nil {
		h.clusterDegraded(err)
		transient = true
	}
	switch {
	case err == nil:
		c.leaseUntil = now.Add(state.config.ConnectionTTL)
		return true
	case c.ctx.Err() != nil:
		return false
	case transient && time.Now().Add(state.config.OperationTimeout).Before(c.leaseUntil):
		return true
	default:
		c.cancel()
		return false
	}
}

// Disconnect releases every remote membership in one atomic operation before
// per-channel domain cleanup. A failed release is counted and reconciled: the
// lease expires by TTL and affected presence scopes are refreshed locally.
func (c *connectionState) closeCluster(subscriptions []*subscriptionState) {
	if !c.clusterOpened {
		return
	}
	c.clusterOpened = false
	h := c.hub
	state := h.cluster
	var scopes []Scope
	for _, subscription := range subscriptions {
		if subscription.member != "" {
			scopes = append(scopes, subscription.key.scope())
		}
	}
	err := h.clusterCall(context.Background(), func(ctx context.Context) error {
		if err := state.backend.WebSocketClose(ctx, state.key, state.instance, c.id); err != nil {
			return err
		}
		for i := range scopes {
			if err := h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPresenceChanged, Scope: &scopes[i]}); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		return
	}
	h.counters.cleanupFailures.Add(1)
	h.mu.Lock()
	marked := false
	for _, scope := range scopes {
		marked = h.markPresenceLocked(scope.key()) || marked
	}
	h.mu.Unlock()
	if marked {
		h.wakePresence()
	}
}
