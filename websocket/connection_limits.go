package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
)

// Each connection owns one local bucket. Dead connections leave no shared live
// quota entries behind; the shared limiter supplies fixed-window semantics.
func (c *connectionState) prepareRateLimit() error {
	backend, err := memory.New(1, clock.System{})
	if err != nil {
		return err
	}
	config := ratelimit.DefaultConfig(keyspace.Namespace{Application: "foundry.websocket", Environment: "local"})
	config.MaxConcurrent = 1
	config.MaxDeclarations = 1
	config.MaxKeyBytes = 64
	config.Timeout = c.hub.config.OperationTimeout
	store, err := ratelimit.NewStore(backend, config)
	if err != nil {
		return err
	}
	declaration := ratelimit.Define("connection", keyspace.TextKeys[ConnectionID](), c.hub.config.MessageRate)
	c.rate, err = declaration.Bind(store)
	return err
}
func (c *connectionState) allowMessage() bool {
	decision, err := c.rate.Allow(c.ctx, c.id)
	if err != nil {
		c.cancel()
		return false
	}
	if decision.Allowed {
		return true
	}
	c.hub.mu.Lock()
	c.hub.rateRejected++
	c.hub.mu.Unlock()
	c.respond(Response{Type: ErrorResponse, Code: RateLimited})
	return false
}

// Caller holds hub.mu. Count a connection only once for this verified subject,
// regardless of its number of rooms or channels using that guard/provider.
func (c *connectionState) subjectAllowedLocked(subject MemberID) bool {
	count := 0
	for _, connection := range c.hub.connections {
		matched := false
		for _, subscription := range connection.subscriptions {
			if subscription.subjectID == subject {
				matched = true
				break
			}
		}
		if !matched {
			for _, pending := range connection.pending {
				if pending.subscription.subjectID == subject {
					matched = true
					break
				}
			}
		}
		if matched {
			if connection == c {
				return true
			}
			count++
		}
	}
	return count < c.hub.config.MaxConnectionsPerSubject
}

func (c *connectionState) withFreshScope(ctx context.Context, run func(context.Context) error) error {
	if c.hub.authentication == nil {
		return run(ctx)
	}
	scope, err := c.hub.authentication.Registry().NewScope(ctx, c.credentials)
	if err != nil {
		return err
	}
	defer scope.Close()
	return run(scope.Context())
}
