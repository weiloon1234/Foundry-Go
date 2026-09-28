package websocket

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// Hub owns connections, bounded queues and active callbacks. Construction starts
// no I/O or goroutines. Keep borrowed authentication/services alive until Done.
// A Hub must not be copied. Stop is permanent; construct a new Hub to restart.
type Hub struct {
	clusterPresence                                                 map[subscriptionKey]PresenceSnapshot
	registry                                                        *Registry
	config                                                          Config
	authentication                                                  *foundryhttp.Authentication
	ctx                                                             context.Context
	cancel                                                          context.CancelFunc
	mu                                                              sync.Mutex
	closing                                                         bool
	closed                                                          bool
	done                                                            chan struct{}
	connections                                                     map[ConnectionID]*connectionState
	ipConnections                                                   map[netip.Addr]int
	metrics                                                         map[ChannelID]*channelCounters
	cluster                                                         *clusterState
	background                                                      int
	degraded                                                        bool
	terminal                                                        error
	history                                                         map[ChannelID][]historyFrame
	historyBytes                                                    map[ChannelID]int
	heartbeatFailures, rateRejected, revocations, forcedDisconnects uint64
	presence                                                        map[subscriptionKey]map[MemberID]*presenceMember
	operations                                                      int
	server                                                          *foundryhttp.Server
	serverSelected                                                  chan struct{}
	accepted, rejected, publications, slowConsumers, failures       uint64
}
type operationKey struct{}
type operationFrame struct {
	hub    *Hub
	parent *operationFrame
	active atomic.Bool
}

func New(registry *Registry, authentication *foundryhttp.Authentication, config Config) (*Hub, error) {
	return newHub(registry, authentication, config, true)
}
func newHub(registry *Registry, authentication *foundryhttp.Authentication, config Config, inbound bool) (*Hub, error) {
	if registry == nil || len(registry.channels) == 0 {
		return nil, fault.New(fault.Invalid, "WebSocket hub requires a registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var guards *auth.Registry
	if authentication != nil {
		guards = authentication.Registry()
		if err := guards.Validate(); err != nil {
			return nil, err
		}
	}
	for _, d := range registry.ordered {
		if inbound {
			if err := d.validateAuth(guards); err != nil {
				return nil, err
			}
		}
	}
	historyBytes := 0
	for _, d := range registry.ordered {
		if d.replay.Messages > config.OutboundQueue-1 || d.replay.Messages > config.DeduplicationEntries {
			return nil, fault.New(fault.Invalid, "replay count exceeds the connection queue or deduplication bound")
		}
		historyBytes += d.replay.Bytes
	}
	if historyBytes > 256<<20 || int64(config.MaxConnections)*int64(config.DeduplicationEntries)*64 > 128<<20 {
		return nil, fault.New(fault.Invalid, "WebSocket history or deduplication budget exceeds its bound")
	}
	ctx, cancel := context.WithCancel(context.Background())
	metrics := make(map[ChannelID]*channelCounters, len(registry.channels))
	for id := range registry.channels {
		metrics[id] = &channelCounters{}
	}
	return &Hub{registry: registry, config: config.snapshot(), authentication: authentication, ctx: ctx, cancel: cancel, done: make(chan struct{}), serverSelected: make(chan struct{}), connections: make(map[ConnectionID]*connectionState), ipConnections: make(map[netip.Addr]int), metrics: metrics, history: make(map[ChannelID][]historyFrame), historyBytes: make(map[ChannelID]int), presence: make(map[subscriptionKey]map[MemberID]*presenceMember)}, nil
}

func (h *Hub) validateChannel(token *channelToken, id ChannelID) error {
	if h == nil || h.done == nil || token == nil {
		return fault.New(fault.Invalid, "WebSocket hub/channel is not initialized")
	}
	d := h.registry.channels[id]
	if d == nil || d.token != token {
		return fault.New(fault.Missing, "channel declaration is not registered")
	}
	return nil
}
func (h *Hub) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}

// Stop seals admission, cancels sockets and waits for actual handler/hook exit.
// Its context bounds waiting, not callback ownership. Calling it from one of this
// Hub's active callbacks is rejected instead of waiting on itself.
func (h *Hub) Stop(ctx context.Context) error {
	if h == nil || h.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "WebSocket stop requires a hub and context")
	}
	for frame, _ := ctx.Value(operationKey{}).(*operationFrame); frame != nil; frame = frame.parent {
		if frame.hub == h && frame.active.Load() {
			return fault.New(fault.Cycle, "WebSocket callback cannot wait for its own shutdown")
		}
	}
	h.beginStop()
	select {
	case <-h.done:
		return h.terminal
	default:
	}
	select {
	case <-h.done:
		return h.terminal
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *Hub) beginStop() {
	h.mu.Lock()
	if !h.closing {
		h.closing = true
		for _, connection := range h.connections {
			connection.startDrainLocked()
		}
	}
	h.cancel()
	h.completeLocked()
	h.mu.Unlock()
}
func (h *Hub) completeLocked() {
	if h.closing && !h.closed && len(h.connections) == 0 && h.operations == 0 && h.background == 0 {
		h.closed = true
		clear(h.history)
		clear(h.historyBytes)
		clear(h.clusterPresence)
		close(h.done)
	}
}

func ownedContext(ctx context.Context, h *Hub) (context.Context, func()) {
	parent, _ := ctx.Value(operationKey{}).(*operationFrame)
	frame := &operationFrame{hub: h, parent: parent}
	frame.active.Store(true)
	return context.WithValue(ctx, operationKey{}, frame), func() { frame.active.Store(false) }
}

// Public codec/inspection work shares one bounded owner and shutdown path.
func (h *Hub) operation(ctx context.Context) (context.Context, func(), error) {
	if h == nil || h.done == nil || ctx == nil {
		return nil, nil, fault.New(fault.Invalid, "WebSocket operation requires a hub and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		return nil, nil, Stopping
	}
	if h.operations >= h.config.MaxConnections {
		h.mu.Unlock()
		return nil, nil, CapacityExceeded
	}
	h.operations++
	h.mu.Unlock()
	ctx, unlink := contextlink.Link(ctx, h.ctx)
	ctx, cancel := context.WithTimeout(ctx, h.config.OperationTimeout)
	ctx, finish := ownedContext(ctx, h)
	return ctx, func() {
		finish()
		cancel()
		unlink()
		h.mu.Lock()
		h.operations--
		h.completeLocked()
		h.mu.Unlock()
	}, nil
}

type Snapshot struct {
	Distributed, Degraded                                     bool
	BackgroundTasks                                           int
	Stopping                                                  bool
	Connections                                               int
	Subscriptions                                             int
	ActiveOperations                                          int
	Accepted, Rejected, Publications, SlowConsumers, Failures uint64
}

// Snapshot is local operational metadata. It contains no credentials or models.
func (h *Hub) Snapshot() Snapshot {
	if h == nil {
		return Snapshot{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}
func (h *Hub) snapshotLocked() Snapshot {
	subscriptions := 0
	for _, c := range h.connections {
		subscriptions += len(c.subscriptions)
	}
	return Snapshot{Distributed: h.cluster != nil, Degraded: h.degraded, BackgroundTasks: h.background, Stopping: h.closing, Connections: len(h.connections), Subscriptions: subscriptions, ActiveOperations: h.operations, Accepted: h.accepted, Rejected: h.rejected, Publications: h.publications, SlowConsumers: h.slowConsumers, Failures: h.failures}
}
func (h *Hub) Registry() *Registry {
	if h == nil {
		return nil
	}
	return h.registry
}
