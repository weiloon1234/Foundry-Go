package websocket

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ReplayConfig retains a recent per-channel bucket. Room replay filters that
// bucket; traffic in other rooms can evict older messages. Zero disables history.
// TTL is a duration in nanoseconds in Go/JSON metadata, not a reconnect cursor.
type ReplayConfig struct {
	Messages int           `json:"messages"`
	Bytes    int           `json:"bytes"`
	TTL      time.Duration `json:"ttl"`
}

func (c ReplayConfig) Validate() error {
	if c == (ReplayConfig{}) {
		return nil
	}
	if c.Messages < 1 || c.Messages > MaxReplayMessages || c.Bytes < 1 || c.Bytes > 4<<20 || c.TTL < time.Millisecond || c.TTL > 24*time.Hour || c.TTL%time.Millisecond != 0 {
		return fault.New(fault.Invalid, "invalid WebSocket replay bounds")
	}
	return nil
}

type historyFrame struct {
	response Response
	data     []byte
	expires  time.Time
}

// These helpers run under hub.mu. Only one source owns local publication,
// replay routing and ID suppression; distributed receive feeds the same paths.
func (h *Hub) pruneHistoryLocked(channel ChannelID, now time.Time) {
	frames := h.history[channel]
	for len(frames) > 0 && !now.Before(frames[0].expires) {
		h.historyBytes[channel] -= len(frames[0].data)
		frames[0] = historyFrame{}
		frames = frames[1:]
	}
	if len(frames) == 0 {
		delete(h.history, channel)
		delete(h.historyBytes, channel)
	} else {
		h.history[channel] = frames
	}
}
func (h *Hub) retainLocked(response Response, data []byte) error {
	policy := h.registry.channels[response.Channel].replay
	if policy.Messages == 0 {
		return nil
	}
	if len(data) > policy.Bytes {
		return fault.New(fault.Invalid, "publication exceeds its channel replay byte bound")
	}
	now := time.Now()
	h.pruneHistoryLocked(response.Channel, now)
	frames := h.history[response.Channel]
	for len(frames) > 0 && (len(frames) >= policy.Messages || h.historyBytes[response.Channel]+len(data) > policy.Bytes) {
		h.historyBytes[response.Channel] -= len(frames[0].data)
		frames[0] = historyFrame{}
		frames = frames[1:]
	}
	h.history[response.Channel] = append(frames, historyFrame{response, data, now.Add(policy.TTL)})
	h.historyBytes[response.Channel] += len(data)
	return nil
}
func reaches(response Response, key subscriptionKey) bool {
	return response.Channel == key.channel && (response.Room == nil || (key.hasRoom && *response.Room == key.room))
}
func (h *Hub) replayLocked(key subscriptionKey, count int) []historyFrame {
	if count == 0 {
		return nil
	}
	h.pruneHistoryLocked(key.channel, time.Now())
	frames := h.history[key.channel]
	result := make([]historyFrame, 0, min(count, len(frames)))
	for i := len(frames) - 1; i >= 0 && len(result) < count; i-- {
		if reaches(frames[i].response, key) {
			result = append(result, frames[i])
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
func (c *connectionState) deliverPublicationLocked(response Response, data []byte, replayed bool) bool {
	if c.seen[response.MessageID] {
		return false
	}
	if replayed {
		response.Replayed = true
		var err error
		data, err = c.hub.encode(response)
		if err != nil {
			c.cancel()
			return false
		}
	}
	if !c.enqueueLocked(data) {
		return false
	}
	if c.seen == nil {
		c.seen = make(map[MessageID]bool)
	}
	if len(c.seenOrder) == c.hub.config.DeduplicationEntries {
		delete(c.seen, c.seenOrder[c.seenNext])
		c.seenOrder[c.seenNext] = response.MessageID
		c.seenNext = (c.seenNext + 1) % len(c.seenOrder)
	} else {
		c.seenOrder = append(c.seenOrder, response.MessageID)
	}
	c.seen[response.MessageID] = true
	if metric := c.hub.metrics[response.Channel]; metric != nil {
		metric.Delivered++
		if replayed {
			metric.Replayed++
		}
	}
	return true
}
func (h *Hub) routePublicationLocked(response Response, data []byte) {
	for _, connection := range h.connections {
		matched := false
		for subscription := range connection.subscriptions {
			if reaches(response, subscription) {
				connection.deliverPublicationLocked(response, data, false)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for key, pending := range connection.pending {
			if reaches(response, key) {
				connection.bufferPendingLocked(pending, response, data)
			}
		}
	}
}
