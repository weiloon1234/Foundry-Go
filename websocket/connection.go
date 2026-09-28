package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

type subscriptionKey struct {
	channel ChannelID
	room    string
	hasRoom bool
}

func requestKey(r Request) subscriptionKey {
	k := subscriptionKey{channel: r.Channel}
	if r.Room != nil {
		k.room = *r.Room
		k.hasRoom = true
	}
	return k
}
func (k subscriptionKey) roomPointer() *string {
	if !k.hasRoom {
		return nil
	}
	room := k.room
	return &room
}

type subscriptionState struct {
	clusterJoined bool
	key           subscriptionKey
	channel       *channelDefinition
	subject       SubjectReference
	origin        attribution.Origin
	member        MemberID
	subjectID     MemberID
}
type connectionState struct {
	clusterOpened    bool
	pending          map[subscriptionKey]*pendingSubscription
	hub              *Hub
	id               ConnectionID
	ip               netip.Addr
	ctx              context.Context
	cancel           context.CancelFunc
	operationCancel  context.CancelFunc
	transportContext context.Context
	rate             ratelimit.Limiter[ConnectionID]
	authFreshUntil   atomic.Int64
	queuedBytes      int
	drainTimer       *time.Timer
	seen             map[MessageID]bool
	seenOrder        []MessageID
	seenNext         int
	socket           *transport.Conn
	credentials      auth.Credentials
	inbound          chan []byte
	outbound         chan []byte
	// Subscriptions are protected by hub.mu, even though inbound work is serial.
	subscriptions map[subscriptionKey]*subscriptionState
}

func (c *connectionState) run() {
	var loops sync.WaitGroup
	loops.Go(c.read)
	loops.Go(c.write)
	loops.Go(c.heartbeat)
	loops.Go(c.refreshAuthorization)
	defer func() { c.operationCancel(); loops.Wait(); c.cancel(); _ = c.socket.CloseNow() }()
	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.inbound:
			if c.ctx.Err() != nil {
				return
			}
			if !c.allowMessage() {
				continue
			}
			request, err := DecodeRequest(data, c.hub.config.MaxFrameBytes)
			if err != nil {
				code, ok := err.(Code)
				if !ok {
					code = Malformed
				}
				c.respond(Response{Type: ErrorResponse, ID: request.ID, Code: code})
				continue
			}
			c.process(request)
		}
	}
}
func (c *connectionState) read() {
	defer c.cancel()
	for {
		kind, data, err := c.socket.Read(c.transportContext)
		if err != nil {
			return
		}
		if kind != transport.MessageText {
			_ = c.socket.CloseNow()
			return
		}
		select {
		case c.inbound <- data:
		case <-c.ctx.Done():
			return
		default:
			_ = c.socket.CloseNow()
			return
		}
	}
}
func (c *connectionState) write() {
	defer c.cancel()
	defer c.socket.CloseNow()
	for {
		select {
		case <-c.ctx.Done():
			c.hub.mu.Lock()
			stopping := c.hub.closing
			c.hub.mu.Unlock()
			if stopping && c.transportContext.Err() == nil {
				c.drain()
			}
			return
		case data := <-c.outbound:
			c.dequeued(data)
			ctx, cancel := context.WithTimeout(c.transportContext, c.hub.config.WriteTimeout)
			err := c.socket.Write(ctx, transport.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
func (h *Hub) encode(response Response) ([]byte, error) {
	response.Version = ProtocolVersion
	data, err := json.Marshal(response)
	if err != nil || len(data) > h.config.MaxFrameBytes {
		return nil, fault.New(fault.Invalid, "WebSocket output exceeds its frame bound")
	}
	return data, nil
}
func (c *connectionState) respond(response Response) bool {
	data, err := c.hub.encode(response)
	if err != nil {
		c.cancel()
		return false
	}
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	return c.enqueueLocked(data)
}
func (c *connectionState) enqueueLocked(data []byte) bool {
	if c.ctx.Err() != nil || c.hub.closing {
		return false
	}
	select {
	case c.outbound <- data:
		c.queuedBytes += len(data)
		return true
	default:
		c.hub.slowConsumers++
		c.cancel()
		return false
	}
}
func (c *connectionState) cleanup() {
	c.hub.mu.Lock()
	if c.drainTimer != nil {
		c.drainTimer.Stop()
	}
	removed := make([]*subscriptionState, 0, len(c.subscriptions))
	for _, subscription := range c.subscriptions {
		c.removeLocked(subscription)
		removed = append(removed, subscription)
	}
	c.hub.mu.Unlock()
	c.closeCluster(removed)
	for _, subscription := range removed {
		subscription.clusterJoined = false
		c.leave(subscription)
	}
	c.credentials = auth.Credentials{}
}
func (c *connectionState) leave(subscription *subscriptionState) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.hub.config.OperationTimeout)
	defer cancel()
	ctx, err := attribution.WithContext(ctx, subscription.origin)
	if err != nil {
		return err
	}
	ctx, finish := ownedContext(ctx, c.hub)
	defer finish()
	err = callback.Isolated("WebSocket leave hook", func() error {
		remote := c.leaveCluster(ctx, subscription)
		local := subscription.channel.leave(ctx, c.hub, c.id, subscription.key.roomPointer(), subscription.subject)
		return errors.Join(remote, local)
	})
	if err != nil || ctx.Err() != nil {
		c.hub.mu.Lock()
		c.hub.failures++
		c.hub.mu.Unlock()
		if err == nil {
			return ctx.Err()
		}
	}
	return err
}
