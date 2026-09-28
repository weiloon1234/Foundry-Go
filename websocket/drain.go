package websocket

import (
	"context"
	"time"

	transport "github.com/coder/websocket"
)

// Admission is sealed before this timer is armed. It also bounds a data write
// already in progress when shutdown begins, rather than only later queue reads.
func (c *connectionState) startDrainLocked() {
	if c.drainTimer == nil {
		c.drainTimer = time.AfterFunc(c.hub.config.DrainTimeout, c.cancel)
	}
}
func (c *connectionState) dequeued(data []byte) {
	c.hub.mu.Lock()
	c.queuedBytes -= len(data)
	c.hub.mu.Unlock()
}
func (c *connectionState) drain() {
	ctx, cancel := context.WithTimeout(c.transportContext, c.hub.config.DrainTimeout)
	defer cancel()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case data := <-c.outbound:
			c.dequeued(data)
			if c.socket.Write(ctx, transport.MessageText, data) != nil {
				return
			}
		default:
			// The active transport reader observes the drain deadline and closes
			// the socket if the peer does not complete the close handshake.
			_ = c.socket.Close(transport.StatusGoingAway, "server stopping")
			return
		}
	}
}
