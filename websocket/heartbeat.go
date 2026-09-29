package websocket

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// maintain owns one timer per connection for pings, authorization refresh and
// cluster lease renewal. A revocation timer, which needs no goroutine, closes
// the connection when authorization is not refreshed in time, even while an
// uncooperative refresh callback still runs and remains owned here.
func (c *connectionState) maintain() {
	h := c.hub
	now := time.Now()
	c.armAuthorization(now)
	defer c.authTimer.Stop()
	nextPing := now.Add(h.config.HeartbeatInterval)
	nextRefresh := now.Add(refreshJitter(h.config.AuthRefreshInterval))
	var nextTouch time.Time
	if h.cluster != nil {
		c.leaseUntil = now.Add(h.cluster.config.ConnectionTTL)
		nextTouch = now.Add(h.cluster.config.ConnectionTTL / 3)
	}
	timer := time.NewTimer(time.Until(earliest(nextPing, nextRefresh, nextTouch)))
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case now = <-timer.C:
		}
		if !now.Before(nextRefresh) {
			if !c.refreshAuthorization() {
				return
			}
			nextRefresh = time.Now().Add(refreshJitter(h.config.AuthRefreshInterval))
		}
		if !nextTouch.IsZero() && !now.Before(nextTouch) {
			if !c.touchCluster(time.Now()) {
				return
			}
			nextTouch = time.Now().Add(h.cluster.config.ConnectionTTL / 3)
		}
		if !now.Before(nextPing) {
			ctx, cancel := context.WithTimeout(c.transportContext, h.config.PongTimeout)
			err := c.socket.Ping(ctx)
			cancel()
			if err != nil {
				if c.ctx.Err() == nil {
					h.counters.heartbeatFailures.Add(1)
					c.cancel()
				}
				return
			}
			nextPing = time.Now().Add(h.config.HeartbeatInterval)
		}
		timer.Reset(time.Until(earliest(nextPing, nextRefresh, nextTouch)))
	}
}

func earliest(times ...time.Time) time.Time {
	var first time.Time
	for _, value := range times {
		if !value.IsZero() && (first.IsZero() || value.Before(first)) {
			first = value
		}
	}
	return first
}

// refreshJitter spreads refreshes over ±10% of the interval so connections
// admitted together do not refresh authorization in lockstep.
func refreshJitter(interval time.Duration) time.Duration {
	spread := interval / 5
	if spread <= 0 {
		return interval
	}
	return interval - spread/2 + rand.N(spread+1)
}

// authWindow bounds one refresh cycle: the jittered interval, the refresh
// itself and a ping or lease renewal that may run first on the same timer.
func (h *Hub) authWindow() time.Duration {
	window := h.config.AuthRefreshInterval + h.config.AuthRefreshInterval/10 + h.config.OperationTimeout + h.config.PongTimeout
	if h.cluster != nil {
		window += h.cluster.config.ConnectionTTL / 3
	}
	return window
}

// armAuthorization extends the freshness window and its revocation deadline.
func (c *connectionState) armAuthorization(now time.Time) {
	window := c.hub.authWindow()
	c.authFreshUntil.Store(now.Add(window).UnixNano())
	if c.authTimer == nil {
		c.authTimer = time.AfterFunc(window, c.revokeStale)
		return
	}
	c.authTimer.Reset(window)
}
func (c *connectionState) revokeStale() {
	if c.ctx.Err() != nil || time.Now().UnixNano() <= c.authFreshUntil.Load() {
		return
	}
	c.hub.counters.revocations.Add(1)
	c.cancel()
}

// Authentication refresh re-resolves credentials in a fresh scope and re-runs
// every private subscription's authorization. Success replaces the scope and
// cached authorization used by incoming messages until the next refresh.
func (c *connectionState) refreshAuthorization() bool {
	h := c.hub
	if h.authentication == nil {
		c.armAuthorization(time.Now())
		return true
	}
	scope, err := h.authentication.Registry().NewScope(c.ctx, c.credentials)
	if err != nil {
		return c.revoke()
	}
	ctx, cancel := context.WithTimeout(scope.Context(), h.config.OperationTimeout)
	ctx, finish := ownedContext(ctx, h)
	type refreshed struct {
		subscription *subscriptionState
		access       accessResult
	}
	var results []refreshed
	err = callback.Isolated("WebSocket authorization refresh", func() error {
		h.mu.Lock()
		subscriptions := make([]*subscriptionState, 0, len(c.subscriptions))
		for _, subscription := range c.subscriptions {
			if subscription.channel.private {
				subscriptions = append(subscriptions, subscription)
			}
		}
		h.mu.Unlock()
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
			results = append(results, refreshed{subscription, access.retained()})
		}
		return ctx.Err()
	})
	if err == nil {
		err = ctx.Err()
	}
	finish()
	cancel()
	if err != nil {
		_ = scope.Close()
		return c.revoke()
	}
	// Authorization is fresh once verified; extend the window before waiting
	// for in-flight messages to release the previous scope, so that wait can
	// never make a successful refresh look stale.
	c.armAuthorization(time.Now())
	h.mu.Lock()
	for _, result := range results {
		if c.subscriptions[result.subscription.key] == result.subscription {
			result.subscription.access = result.access
		}
	}
	h.mu.Unlock()
	c.scopeMu.Lock()
	previous := c.scope
	c.scope = scope
	c.scopeMu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	return true
}
func (c *connectionState) revoke() bool {
	if c.ctx.Err() == nil {
		c.hub.counters.revocations.Add(1)
		c.cancel()
	}
	return false
}
