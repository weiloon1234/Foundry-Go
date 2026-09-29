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
	// access is the last fresh authorization of this subscription, refreshed
	// with the connection's credentials. Incoming messages reuse it within the
	// freshness window instead of repeating credential lookup and policy checks.
	// Protected by hub.mu; its context field is never retained.
	access accessResult
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
	// rate is owned by the serial inbound loop.
	rate           tokenBucket
	authFreshUntil atomic.Int64
	authTimer      *time.Timer
	// Byte accounting is atomic so writers and readers never take hub.mu.
	queuedBytes atomic.Int64
	closeStatus atomic.Int32
	// The fields below are protected by hub.mu.
	drainTimer      *time.Timer
	seen            map[MessageID]bool
	seenOrder       []MessageID
	seenNext        int
	routeGeneration uint64
	subscriptions   map[subscriptionKey]*subscriptionState
	// scope is the connection's current authentication scope for incoming
	// messages, replaced by each successful authorization refresh.
	scopeMu     sync.RWMutex
	scope       *auth.Scope
	socket      *transport.Conn
	credentials auth.Credentials
	leaseUntil  time.Time
	inbound     chan []byte
	outbound    chan []byte
}

func (c *connectionState) run() {
	var loops sync.WaitGroup
	loops.Go(c.read)
	loops.Go(c.write)
	loops.Go(c.maintain)
	defer func() { c.operationCancel(); loops.Wait(); c.cancel(); _ = c.socket.CloseNow() }()
	c.rate = newTokenBucket(c.hub.config.MessageRate, time.Now())
	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.inbound:
			c.release(len(data))
			if c.ctx.Err() != nil {
				return
			}
			// Rate limiting precedes decoding, so a flood costs a token check
			// and a scan of its leading members for the request ID.
			if !c.rate.allow(time.Now()) {
				c.hub.counters.rateRejected.Add(1)
				c.respond(Response{Type: ErrorResponse, ID: leadingRequestID(data), Code: RateLimited})
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
		if kind != transport.MessageText || !c.reserve(len(data)) {
			c.hub.counters.slowConsumers.Add(1)
			_ = c.socket.CloseNow()
			return
		}
		select {
		case c.inbound <- data:
		case <-c.ctx.Done():
			c.release(len(data))
			return
		default:
			// The client exceeded the exported inbound queue.
			c.release(len(data))
			c.hub.counters.slowConsumers.Add(1)
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
			if status := transport.StatusCode(c.closeStatus.Load()); status != 0 && c.transportContext.Err() == nil {
				c.drain(status)
			}
			return
		case data := <-c.outbound:
			c.release(len(data))
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

// reserveResult says which budget, if any, refused a frame.
type reserveResult uint8

const (
	reserved reserveResult = iota
	connectionBudgetFull
	hubBudgetFull
)

// reserve admits bytes against both the connection and hub budgets. Frames
// shared by several connections count once per connection.
func (c *connectionState) reserve(bytes int) bool { return c.tryReserve(bytes) == reserved }
func (c *connectionState) tryReserve(bytes int) reserveResult {
	n := int64(bytes)
	if c.queuedBytes.Add(n) > int64(c.hub.config.MaxQueuedBytes) {
		c.queuedBytes.Add(-n)
		return connectionBudgetFull
	}
	if c.hub.queuedBytes.Add(n) > c.hub.config.MaxTotalQueuedBytes {
		c.hub.queuedBytes.Add(-n)
		c.queuedBytes.Add(-n)
		return hubBudgetFull
	}
	return reserved
}
func (c *connectionState) release(bytes int) {
	c.queuedBytes.Add(-int64(bytes))
	c.hub.queuedBytes.Add(-int64(bytes))
}

func (c *connectionState) enqueueLocked(data []byte) bool {
	if c.ctx.Err() != nil || c.hub.closing {
		return false
	}
	result := c.tryReserve(len(data))
	// An exhausted hub budget disconnects the connections actually holding it
	// (the largest queues) before this one, unless this one holds the most.
	for result == hubBudgetFull && c.hub.evictLargestLocked(c) {
		result = c.tryReserve(len(data))
	}
	if result == reserved {
		select {
		case c.outbound <- data:
			return true
		default:
			c.release(len(data))
		}
	}
	c.hub.counters.slowConsumers.Add(1)
	c.cancel()
	return false
}

// evictLargestLocked disconnects the connection with the most queued bytes
// when it holds more than current, releasing its queues immediately so the
// hub budget recovers. It reports false when current is itself the largest.
func (h *Hub) evictLargestLocked(current *connectionState) bool {
	var victim *connectionState
	largest := current.queuedBytes.Load()
	for _, connection := range h.connections {
		if held := connection.queuedBytes.Load(); held > largest && connection.ctx.Err() == nil {
			victim, largest = connection, held
		}
	}
	if victim == nil {
		return false
	}
	h.counters.slowConsumers.Add(1)
	victim.cancel()
	// The victim's loops exit on cancellation; frames taken here are simply
	// never written. Receiving from its channels is safe concurrently.
	victim.releaseQueues()
	return true
}

// releaseQueues returns bytes still queued after every loop exited and the
// connection left all hub indexes, so no producer can reach it any more.
func (c *connectionState) releaseQueues() {
	for {
		select {
		case data := <-c.outbound:
			c.release(len(data))
		case data := <-c.inbound:
			c.release(len(data))
		default:
			return
		}
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
	for key := range c.pending {
		c.dropPendingLocked(key)
	}
	c.hub.mu.Unlock()
	c.closeCluster(removed)
	for _, subscription := range removed {
		subscription.clusterJoined = false
		c.leave(subscription)
	}
	c.scopeMu.Lock()
	scope := c.scope
	c.scope = nil
	c.scopeMu.Unlock()
	if scope != nil {
		_ = scope.Close()
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
		c.hub.counters.failures.Add(1)
		if err == nil {
			return ctx.Err()
		}
	}
	return err
}
