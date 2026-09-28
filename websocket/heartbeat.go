package websocket

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func (c *connectionState) heartbeat() {
	period := min(c.hub.config.HeartbeatInterval, c.hub.config.AuthRefreshInterval/2)
	if c.hub.cluster != nil {
		period = min(period, c.hub.cluster.config.ConnectionTTL/3)
	}
	timer := time.NewTicker(period)
	defer timer.Stop()
	nextPing := time.Now().Add(c.hub.config.HeartbeatInterval)
	for {
		select {
		case <-c.ctx.Done():
			return
		case now := <-timer.C:
			if until := c.authFreshUntil.Load(); until != 0 && now.UnixNano() > until {
				c.hub.mu.Lock()
				c.hub.revocations++
				c.hub.mu.Unlock()
				c.cancel()
				return
			}
			if c.hub.cluster != nil {
				if err := c.maintainCluster(); err != nil {
					if c.ctx.Err() == nil {
						c.cancel()
					}
					return
				}
			}
			if now.Before(nextPing) {
				continue
			}
			ctx, cancel := context.WithTimeout(c.transportContext, c.hub.config.PongTimeout)
			err := c.socket.Ping(ctx)
			cancel()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.hub.mu.Lock()
				c.hub.heartbeatFailures++
				c.hub.mu.Unlock()
				c.cancel()
				return
			}
			nextPing = time.Now().Add(c.hub.config.HeartbeatInterval)
		}
	}
}

// Authentication refresh is separately owned so a busy message handler cannot
// suppress revocation checks. Callbacks remain bounded by connection ownership;
// heartbeat aborts transport if an uncooperative refresh misses its deadline.
func (c *connectionState) refreshAuthorization() {
	c.authFreshUntil.Store(time.Now().Add(c.hub.config.AuthRefreshInterval + c.hub.config.OperationTimeout).UnixNano())
	timer := time.NewTicker(c.hub.config.AuthRefreshInterval)
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
		}
		ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.OperationTimeout)
		ctx, finish := ownedContext(ctx, c.hub)
		err := callback.Isolated("WebSocket authorization refresh", func() error {
			return c.withFreshScope(ctx, func(ctx context.Context) error {
				c.hub.mu.Lock()
				subscriptions := make([]*subscriptionState, 0, len(c.subscriptions))
				for _, subscription := range c.subscriptions {
					if subscription.channel.private {
						subscriptions = append(subscriptions, subscription)
					}
				}
				c.hub.mu.Unlock()
				for _, subscription := range subscriptions {
					access, err := subscription.channel.check(ctx, subscription.key.roomPointer())
					if err != nil {
						return err
					}
					actual, err := memberID(access.reference)
					if err != nil {
						return err
					}
					if actual != subscription.subjectID {
						return Unauthenticated
					}
				}
				return ctx.Err()
			})
		})
		if err == nil {
			err = ctx.Err()
		}
		finish()
		cancel()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			c.hub.mu.Lock()
			c.hub.revocations++
			c.hub.mu.Unlock()
			c.cancel()
			return
		}
		c.authFreshUntil.Store(time.Now().Add(c.hub.config.AuthRefreshInterval + c.hub.config.OperationTimeout).UnixNano())
	}
}
