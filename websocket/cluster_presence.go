package websocket

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (h *Hub) validateClusterPresence(ctx context.Context, key subscriptionKey, snapshot PresenceSnapshot) (PresenceSnapshot, error) {
	channel := h.registry.channels[key.channel]
	if channel == nil || channel.memberWire == nil || snapshot.Revision == 0 || snapshot.Revision > 1<<53-1 || len(snapshot.Members) > h.config.MaxPresenceMembers {
		return PresenceSnapshot{}, fault.New(fault.Invalid, "invalid cluster presence snapshot")
	}
	seen := make(map[MemberID]bool, len(snapshot.Members))
	total := 0
	members := make([]MemberFrame, 0, len(snapshot.Members))
	limits := h.config.Payload
	limits.Bytes = h.config.MaxMemberBytes
	for _, frame := range snapshot.Members {
		if !validDigest(string(frame.ID)) || seen[frame.ID] || frame.Connections < 1 || frame.Connections > h.cluster.config.MaxConnections {
			return PresenceSnapshot{}, fault.New(fault.Invalid, "invalid cluster member metadata")
		}
		seen[frame.ID] = true
		total += frame.Connections
		if total > h.cluster.config.MaxConnections {
			return PresenceSnapshot{}, fault.New(fault.Invalid, "cluster presence exceeds connection capacity")
		}
		frame.Data = slices.Clone(frame.Data)
		if err := channel.memberWire(ctx, frame.Data, limits); err != nil {
			return PresenceSnapshot{}, err
		}
		members = append(members, frame)
	}
	slices.SortFunc(members, func(a, b MemberFrame) int { return cmp.Compare(a.ID, b.ID) })
	return PresenceSnapshot{Revision: snapshot.Revision, Members: members}, ctx.Err()
}
func (h *Hub) clusterMembers(ctx context.Context, key subscriptionKey) (PresenceSnapshot, error) {
	var result PresenceSnapshot
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		var err error
		result, err = h.cluster.backend.WebSocketMembers(ctx, h.cluster.key, key.scope())
		if err != nil {
			return err
		}
		result, err = h.validateClusterPresence(ctx, key, result)
		return err
	})
	return result, err
}
func (h *Hub) refreshClusterPresence(ctx context.Context, key subscriptionKey) error {
	h.mu.Lock()
	_, active := h.clusterPresence[key]
	h.mu.Unlock()
	if !active {
		return nil
	}
	snapshot, err := h.clusterMembers(ctx, key)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, active := h.clusterPresence[key]; active && !h.closing {
		h.applyPresenceLocked(key, snapshot)
	}
	return nil
}

// markPresenceLocked queues an active scope for the presence worker. Inactive
// scopes are ignored, so the queue never exceeds MaxPresenceScopes.
func (h *Hub) markPresenceLocked(key subscriptionKey) bool {
	if _, active := h.clusterPresence[key]; !active {
		return false
	}
	h.cluster.dirty[key] = struct{}{}
	return true
}
func (h *Hub) wakePresence() {
	select {
	case h.cluster.wake <- struct{}{}:
	default:
	}
}

// presenceDebounce coalesces bursts of presence changes for one refresh.
const presenceDebounce = 20 * time.Millisecond
const presenceConcurrency = 4

// startPresenceWorker owns authority refreshes outside the receive loop. It
// refreshes queued scopes after a short debounce and reconciles every active
// scope once per heartbeat period, covering missed changes and expired leases.
func (h *Hub) startPresenceWorker(ctx context.Context) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		return
	}
	h.background++
	h.mu.Unlock()
	go func() {
		defer func() {
			h.mu.Lock()
			h.background--
			h.completeLocked()
			h.mu.Unlock()
		}()
		reconcile := time.NewTicker(h.cluster.config.ConnectionTTL / 3)
		defer reconcile.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reconcile.C:
				h.mu.Lock()
				for key := range h.clusterPresence {
					h.cluster.dirty[key] = struct{}{}
				}
				h.mu.Unlock()
			case <-h.cluster.wake:
				debounce := time.NewTimer(presenceDebounce)
				select {
				case <-ctx.Done():
					debounce.Stop()
					return
				case <-debounce.C:
				}
			}
			h.mu.Lock()
			keys := make([]subscriptionKey, 0, len(h.cluster.dirty))
			for key := range h.cluster.dirty {
				keys = append(keys, key)
			}
			clear(h.cluster.dirty)
			h.mu.Unlock()
			slots := make(chan struct{}, presenceConcurrency)
			var refreshes sync.WaitGroup
			for _, key := range keys {
				if ctx.Err() != nil {
					break
				}
				slots <- struct{}{}
				refreshes.Go(func() {
					defer func() { <-slots }()
					// Failures are counted by clusterCall; the next change or
					// reconcile retries, so one slow scope never blocks others.
					_ = h.refreshClusterPresence(ctx, key)
				})
			}
			refreshes.Wait()
		}
	}()
}
func (h *Hub) applyPresenceLocked(key subscriptionKey, snapshot PresenceSnapshot) {
	current := h.clusterPresence[key]
	if current.Revision >= snapshot.Revision {
		return
	}
	before := make(map[MemberID]MemberFrame, len(current.Members))
	for _, member := range current.Members {
		before[member.ID] = member
	}
	for _, member := range snapshot.Members {
		prior, present := before[member.ID]
		if !present {
			h.presenceChangedLocked(key, PresenceJoined, member)
		} else if prior.Connections != member.Connections || !bytes.Equal(prior.Data, member.Data) {
			h.presenceChangedLocked(key, PresenceUpdated, member)
		}
		delete(before, member.ID)
	}
	for _, member := range before {
		member.Connections = 0
		h.presenceChangedLocked(key, PresenceLeft, member)
	}
	h.clusterPresence[key] = snapshot
}
