package websocket

import "context"

func (c *connectionState) subscribe(ctx context.Context, request Request, key subscriptionKey, channel *channelDefinition, access accessResult) (Response, error) {
	replayCount := channel.replay.Messages
	if request.Replay != nil {
		replayCount = *request.Replay
	}
	if replayCount > channel.replay.Messages {
		return Response{}, Malformed
	}
	subscription := &subscriptionState{key: key, channel: channel, subject: access.reference, origin: access.origin, access: access.retained()}
	if channel.private {
		var err error
		subscription.subjectID, err = memberID(access.reference)
		if err != nil {
			return Response{}, err
		}
	}
	var data []byte
	if channel.member != nil {
		subscription.member = subscription.subjectID
		limits := c.hub.config.Payload
		limits.Bytes = c.hub.config.MaxMemberBytes
		var err error
		data, err = channel.member(ctx, access, limits)
		if err != nil {
			return Response{}, err
		}
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.hub.closing || c.ctx.Err() != nil {
			return Stopping
		}
		if c.subscriptions[key] != nil {
			return AlreadySubscribed
		}
		if len(c.subscriptions) >= c.hub.config.MaxSubscriptions {
			return CapacityExceeded
		}
		if subscription.subjectID != "" && !c.subjectAllowedLocked(subscription.subjectID) {
			return CapacityExceeded
		}
		if subscription.member != "" {
			if c.hub.cluster == nil {
				group, exists := c.hub.presence[key]
				if !exists && len(c.hub.presence) >= c.hub.config.MaxPresenceScopes {
					return CapacityExceeded
				}
				if group[subscription.member] == nil && len(group) >= c.hub.config.MaxPresenceMembers {
					return CapacityExceeded
				}
			} else if _, exists := c.hub.clusterPresence[key]; !exists && len(c.hub.clusterPresence) >= c.hub.config.MaxPresenceScopes {
				return CapacityExceeded
			}
		}
		return nil
	}
	c.hub.mu.Lock()
	err := check()
	if err == nil {
		c.reservePendingLocked(subscription)
	}
	c.hub.mu.Unlock()
	if err != nil {
		return Response{}, err
	}
	admitted, joined := false, false
	defer func() {
		if !admitted {
			c.hub.mu.Lock()
			c.dropPendingLocked(key)
			c.hub.mu.Unlock()
			if joined {
				_ = c.leave(subscription)
			}
		}
	}()
	if err := channel.join(ctx, c.hub, c.id, access); err != nil {
		return Response{}, err
	}
	joined = true
	var history []historyFrame
	var presence PresenceSnapshot
	if c.hub.cluster != nil {
		subscription.clusterJoined = true // An uncertain join still needs compensation.
		history, presence, err = c.joinCluster(ctx, subscription, data, replayCount)
		if err != nil {
			return Response{}, err
		}
	}
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	if err := check(); err != nil {
		return Response{}, err
	}
	pending := c.pending[key]
	c.hub.promotePendingLocked(c, subscription, pending)
	admitted = true
	var changed *MemberFrame
	kind := PresenceJoined
	if subscription.member != "" && c.hub.cluster == nil {
		group := c.hub.presence[key]
		member := group[subscription.member]
		if member == nil {
			member = &presenceMember{data: data, connections: make(map[ConnectionID]bool)}
			group[subscription.member] = member
		} else {
			kind = PresenceUpdated
			member.data = data
		}
		member.connections[c.id] = true
		changed = &MemberFrame{ID: subscription.member, Data: member.data, Connections: len(member.connections)}
	}
	response := Response{Type: Subscribed, ID: request.ID, Channel: request.Channel, Room: request.Room}
	if channel.member != nil {
		if c.hub.cluster == nil {
			response.Members = c.hub.memberFrames(key)
		} else {
			current := c.hub.clusterPresence[key]
			if current.Revision > presence.Revision {
				presence = current
			}
			response.Members = presence.Members
		}
	}
	encoded, err := c.hub.encode(response)
	if err != nil {
		c.cancel()
		return Response{}, err
	}
	c.enqueueLocked(encoded)
	if c.hub.cluster == nil {
		history = c.hub.replayLocked(key, replayCount)
	}
	for _, frame := range history {
		c.deliverPublicationLocked(frame.response, frame.data, true)
	}
	for _, frame := range pending.frames {
		c.deliverPublicationLocked(frame.response, frame.data, false)
	}
	if changed != nil {
		c.hub.presenceChangedLocked(key, kind, *changed)
	}
	if subscription.member != "" && c.hub.cluster != nil {
		c.hub.applyPresenceLocked(key, presence)
	}
	return Response{}, nil
}
func (c *connectionState) removeLocked(subscription *subscriptionState) {
	delete(c.subscriptions, subscription.key)
	c.hub.unindexSubscriptionLocked(c, subscription.key)
	c.hub.trackSubjectLocked(c, subscription.subjectID, -1)
	if subscription.member == "" {
		return
	}
	if c.hub.cluster != nil {
		c.hub.releasePresenceScopeLocked(subscription.key)
		return
	}
	group := c.hub.presence[subscription.key]
	member := group[subscription.member]
	if member == nil {
		return
	}
	delete(member.connections, c.id)
	frame := MemberFrame{ID: subscription.member, Data: member.data, Connections: len(member.connections)}
	kind := PresenceUpdated
	if frame.Connections == 0 {
		delete(group, subscription.member)
		kind = PresenceLeft
	}
	c.hub.releasePresenceScopeLocked(subscription.key)
	c.hub.presenceChangedLocked(subscription.key, kind, frame)
}
func (h *Hub) presenceChangedLocked(key subscriptionKey, kind ResponseType, member MemberFrame) {
	frame, err := h.encode(Response{Type: kind, Channel: key.channel, Room: key.roomPointer(), Member: &member})
	if err != nil {
		return
	}
	for connection := range h.subscribers[key] {
		connection.enqueueLocked(frame)
	}
}

// Subscription indexes are maintained with every hub.mu mutation so routing
// and presence notification visit only matching connections.
func (h *Hub) indexSubscriptionLocked(c *connectionState, key subscriptionKey) {
	connections := h.subscribers[key]
	if connections == nil {
		connections = make(map[*connectionState]struct{})
		h.subscribers[key] = connections
		keys := h.channelKeys[key.channel]
		if keys == nil {
			keys = make(map[subscriptionKey]struct{})
			h.channelKeys[key.channel] = keys
		}
		keys[key] = struct{}{}
	}
	connections[c] = struct{}{}
}
func (h *Hub) unindexSubscriptionLocked(c *connectionState, key subscriptionKey) {
	connections := h.subscribers[key]
	delete(connections, c)
	if len(connections) == 0 {
		delete(h.subscribers, key)
		keys := h.channelKeys[key.channel]
		delete(keys, key)
		if len(keys) == 0 {
			delete(h.channelKeys, key.channel)
		}
	}
}
