package websocket

import (
	"bytes"
	"cmp"
	"context"
	"slices"

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
func (h *Hub) refreshClusterPresence(ctx context.Context, scope Scope) error {
	key := scope.key()
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
