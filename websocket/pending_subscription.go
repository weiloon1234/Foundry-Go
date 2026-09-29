package websocket

type pendingSubscription struct {
	subscription *subscriptionState
	frames       []historyFrame
	bytes        int
}

func (c *connectionState) reservePendingLocked(subscription *subscriptionState) {
	if c.pending == nil {
		c.pending = make(map[subscriptionKey]*pendingSubscription)
	}
	pending := &pendingSubscription{subscription: subscription}
	c.pending[subscription.key] = pending
	h := c.hub
	connections := h.pendingSubscribers[subscription.key]
	if connections == nil {
		connections = make(map[*connectionState]*pendingSubscription)
		h.pendingSubscribers[subscription.key] = connections
	}
	connections[c] = pending
	h.trackSubjectLocked(c, subscription.subjectID, 1)
	if subscription.member != "" {
		if h.cluster == nil {
			if h.presence[subscription.key] == nil {
				h.presence[subscription.key] = make(map[MemberID]*presenceMember)
			}
		} else if _, exists := h.clusterPresence[subscription.key]; !exists {
			h.clusterPresence[subscription.key] = PresenceSnapshot{}
		}
	}
}

// unindexPendingLocked removes the pending admission and releases its bytes.
func (c *connectionState) unindexPendingLocked(key subscriptionKey) *pendingSubscription {
	pending := c.pending[key]
	if pending == nil {
		return nil
	}
	delete(c.pending, key)
	connections := c.hub.pendingSubscribers[key]
	delete(connections, c)
	if len(connections) == 0 {
		delete(c.hub.pendingSubscribers, key)
	}
	c.release(pending.bytes)
	pending.bytes = 0
	return pending
}
func (c *connectionState) dropPendingLocked(key subscriptionKey) {
	if pending := c.unindexPendingLocked(key); pending != nil {
		c.hub.trackSubjectLocked(c, pending.subscription.subjectID, -1)
	}
	c.hub.releasePresenceScopeLocked(key)
}

// promotePendingLocked turns an admitted pending reservation into an active
// subscription. Its subject slot carries over unchanged.
func (h *Hub) promotePendingLocked(c *connectionState, subscription *subscriptionState, pending *pendingSubscription) {
	if pending != nil {
		c.unindexPendingLocked(subscription.key)
	} else {
		h.trackSubjectLocked(c, subscription.subjectID, 1)
	}
	c.subscriptions[subscription.key] = subscription
	h.indexSubscriptionLocked(c, subscription.key)
}
func (h *Hub) releasePresenceScopeLocked(key subscriptionKey) {
	if len(h.subscribers[key]) != 0 || len(h.pendingSubscribers[key]) != 0 {
		return
	}
	delete(h.presence, key)
	delete(h.clusterPresence, key)
	if h.cluster != nil {
		delete(h.cluster.dirty, key)
	}
}
func (c *connectionState) bufferPendingLocked(pending *pendingSubscription, response Response, data []byte) {
	for _, frame := range pending.frames {
		if frame.response.MessageID == response.MessageID {
			return
		}
	}
	if len(pending.frames) >= c.hub.config.OutboundQueue || !c.reserve(len(data)) {
		c.hub.counters.slowConsumers.Add(1)
		c.cancel()
		return
	}
	pending.frames = append(pending.frames, historyFrame{response: response, data: data})
	pending.bytes += len(data)
}
