package websocket

import (
	"context"
	"time"

	transport "github.com/coder/websocket"
)

// Close statuses for drained connections: hub shutdown, and a lost cluster
// stream after which clients should reconnect and request replay.
const (
	closeGoingAway     = transport.StatusGoingAway
	closeTryAgainLater = transport.StatusTryAgainLater
)

// Admission is sealed before this timer is armed. It also bounds a data write
// already in progress when shutdown begins, rather than only later queue reads.
func (c *connectionState) startDrainLocked(status transport.StatusCode) {
	c.closeStatus.CompareAndSwap(0, int32(status))
	if c.drainTimer == nil {
		c.drainTimer = time.AfterFunc(c.hub.config.DrainTimeout, c.cancel)
	}
}
func (c *connectionState) drain(status transport.StatusCode) {
	ctx, cancel := context.WithTimeout(c.transportContext, c.hub.config.DrainTimeout)
	defer cancel()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case data := <-c.outbound:
			c.release(len(data))
			if c.socket.Write(ctx, transport.MessageText, data) != nil {
				return
			}
		default:
			// The active transport reader observes the drain deadline and closes
			// the socket if the peer does not complete the close handshake.
			reason := "server stopping"
			if status == closeTryAgainLater {
				reason = "realtime stream interrupted"
			}
			_ = c.socket.Close(status, reason)
			return
		}
	}
}
