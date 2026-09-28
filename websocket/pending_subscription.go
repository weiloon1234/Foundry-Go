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
	c.pending[subscription.key] = &pendingSubscription{subscription: subscription}
	if subscription.member != "" {
		if c.hub.cluster == nil {
			if c.hub.presence[subscription.key] == nil {
				c.hub.presence[subscription.key] = make(map[MemberID]*presenceMember)
			}
		} else if _, exists := c.hub.clusterPresence[subscription.key]; !exists {
			c.hub.clusterPresence[subscription.key] = PresenceSnapshot{}
		}
	}
}
func (c *connectionState) dropPendingLocked(key subscriptionKey) {
	delete(c.pending, key)
	c.hub.releasePresenceScopeLocked(key)
}
func (h *Hub) releasePresenceScopeLocked(key subscriptionKey) {
	for _, connection := range h.connections {
		if connection.subscriptions[key] != nil || connection.pending[key] != nil {
			return
		}
	}
	delete(h.presence, key)
	delete(h.clusterPresence, key)
}
func (c *connectionState) bufferPendingLocked(pending *pendingSubscription, response Response, data []byte) {
	for _, frame := range pending.frames {
		if frame.response.MessageID == response.MessageID {
			return
		}
	}
	if len(pending.frames) >= c.hub.config.OutboundQueue {
		c.hub.slowConsumers++
		c.cancel()
		return
	}
	pending.frames = append(pending.frames, historyFrame{response: response, data: data})
	pending.bytes += len(data)
}
