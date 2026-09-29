package websocket

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// Hub owns connections, bounded queues and active callbacks. Construction starts
// no I/O or goroutines. Keep borrowed authentication/services alive until Done.
// A Hub must not be copied. Stop is permanent; construct a new Hub to restart.
type Hub struct {
	registry       *Registry
	config         Config
	authentication *foundryhttp.Authentication
	logger         atomic.Pointer[slog.Logger]
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	serverSelected chan struct{}
	server         *foundryhttp.Server
	// Trusted management/publication work has its own queued admission,
	// independent of socket capacity. Counters and queue budgets are lock-free.
	admission   *admission.Semaphore
	counters    hubCounters
	queuedBytes atomic.Int64
	metrics     map[ChannelID]*channelCounters

	mu         sync.Mutex
	closing    bool
	closed     bool
	background int
	operations int
	degraded   bool
	terminal   error
	cluster    *clusterState
	// Connection and routing indexes below are protected by mu. Publication
	// routing visits only the subscribers of its key; subject and presence-scope
	// checks use counts instead of scanning every connection.
	connections        map[ConnectionID]*connectionState
	ipConnections      map[netip.Addr]int
	subscribers        map[subscriptionKey]map[*connectionState]struct{}
	channelKeys        map[ChannelID]map[subscriptionKey]struct{}
	pendingSubscribers map[subscriptionKey]map[*connectionState]*pendingSubscription
	subjects           map[MemberID]map[*connectionState]int
	routeGeneration    uint64
	history            map[ChannelID][]historyFrame
	historyBytes       map[ChannelID]int
	presence           map[subscriptionKey]map[MemberID]*presenceMember
	clusterPresence    map[subscriptionKey]PresenceSnapshot
}

type hubCounters struct {
	accepted, rejected, publications, slowConsumers, failures              atomic.Uint64
	heartbeatFailures, rateRejected, revocations, forcedDisconnects, gaps  atomic.Uint64
	resubscriptions, droppedEnvelopes, cleanupFailures, operationOverloads atomic.Uint64
}

type operationKey struct{}
type operationFrame struct {
	hub    *Hub
	parent *operationFrame
	active atomic.Bool
}

// Option configures optional hub collaborators at construction.
type Option func(*options) error
type options struct{ logger *slog.Logger }

// WithLogger reports cluster degradation, stream loss/recovery and terminal
// faults with safe structured diagnostics. Without it the hub does not log.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) error {
		if logger == nil {
			return fault.New(fault.Invalid, "WebSocket logger cannot be nil")
		}
		o.logger = logger
		return nil
	}
}

func New(registry *Registry, authentication *foundryhttp.Authentication, config Config, opts ...Option) (*Hub, error) {
	return newHub(registry, authentication, config, true, opts)
}
func newHub(registry *Registry, authentication *foundryhttp.Authentication, config Config, inbound bool, opts []Option) (*Hub, error) {
	if registry == nil || len(registry.channels) == 0 {
		return nil, fault.New(fault.Invalid, "WebSocket hub requires a registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var configured options
	for _, option := range opts {
		if option == nil {
			return nil, fault.New(fault.Invalid, "nil WebSocket option")
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
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
	if historyBytes > 256<<20 || int64(config.MaxConnections)*int64(config.DeduplicationEntries)*64 > 1<<30 {
		return nil, fault.New(fault.Invalid, "WebSocket history or deduplication budget exceeds its bound")
	}
	ctx, cancel := context.WithCancel(context.Background())
	metrics := make(map[ChannelID]*channelCounters, len(registry.channels))
	for id := range registry.channels {
		metrics[id] = &channelCounters{}
	}
	hub := &Hub{registry: registry, config: config.snapshot(), authentication: authentication, ctx: ctx, cancel: cancel, done: make(chan struct{}), serverSelected: make(chan struct{}),
		admission: admission.New(config.MaxOperations), metrics: metrics,
		connections: make(map[ConnectionID]*connectionState), ipConnections: make(map[netip.Addr]int),
		subscribers: make(map[subscriptionKey]map[*connectionState]struct{}), channelKeys: make(map[ChannelID]map[subscriptionKey]struct{}),
		pendingSubscribers: make(map[subscriptionKey]map[*connectionState]*pendingSubscription), subjects: make(map[MemberID]map[*connectionState]int),
		history: make(map[ChannelID][]historyFrame), historyBytes: make(map[ChannelID]int), presence: make(map[subscriptionKey]map[MemberID]*presenceMember)}
	if configured.logger != nil {
		hub.logger.Store(configured.logger)
	}
	return hub, nil
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
// Hub's active callbacks is rejected instead of waiting on itself. It returns the
// unrecoverable cluster fault that terminated the hub, if any.
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
		return h.terminalError()
	default:
	}
	select {
	case <-h.done:
		return h.terminalError()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *Hub) terminalError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.terminal
}
func (h *Hub) beginStop() {
	h.mu.Lock()
	if !h.closing {
		h.closing = true
		for _, connection := range h.connections {
			connection.startDrainLocked(closeGoingAway)
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

// Public codec/inspection work shares one bounded owner and shutdown path. It
// waits briefly for capacity and reports fault.Overloaded instead of failing
// immediately; socket capacity is admitted separately.
func (h *Hub) operation(ctx context.Context) (context.Context, func(), error) {
	if h == nil || h.done == nil || ctx == nil {
		return nil, nil, fault.New(fault.Invalid, "WebSocket operation requires a hub and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := h.admission.Acquire(ctx, admission.Wait(h.config.OperationTimeout), h.ctx.Done()); err != nil {
		if errors.Is(err, fault.Closed) {
			return nil, nil, Stopping
		}
		h.counters.operationOverloads.Add(1)
		return nil, nil, err
	}
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		h.admission.Release()
		return nil, nil, Stopping
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
		h.admission.Release()
		h.mu.Lock()
		h.operations--
		h.completeLocked()
		h.mu.Unlock()
	}, nil
}

type Snapshot struct {
	Distributed, Degraded                                       bool
	Streaming                                                   bool
	BackgroundTasks                                             int
	Stopping                                                    bool
	Connections                                                 int
	Subscriptions                                               int
	ActiveOperations                                            int
	QueuedBytes                                                 int64
	Accepted, Rejected, Publications, SlowConsumers, Failures   uint64
	Gaps, Resubscriptions, DroppedEnvelopes, OperationOverloads uint64
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
	for _, connections := range h.subscribers {
		subscriptions += len(connections)
	}
	result := Snapshot{Distributed: h.cluster != nil, Degraded: h.degraded, BackgroundTasks: h.background, Stopping: h.closing, Connections: len(h.connections), Subscriptions: subscriptions, ActiveOperations: h.operations, QueuedBytes: h.queuedBytes.Load(),
		Accepted: h.counters.accepted.Load(), Rejected: h.counters.rejected.Load(), Publications: h.counters.publications.Load(), SlowConsumers: h.counters.slowConsumers.Load(), Failures: h.counters.failures.Load(),
		Gaps: h.counters.gaps.Load(), Resubscriptions: h.counters.resubscriptions.Load(), DroppedEnvelopes: h.counters.droppedEnvelopes.Load(), OperationOverloads: h.counters.operationOverloads.Load()}
	result.Streaming = h.cluster == nil || h.cluster.streaming
	return result
}
func (h *Hub) Registry() *Registry {
	if h == nil {
		return nil
	}
	return h.registry
}

// Probe reports local serving health for readiness checks without I/O: nil
// while the hub admits connections and, when distributed, its fan-out stream is
// subscribed. It fails while stopping, after a terminal fault or during a
// stream gap/resubscription. Transient per-operation failures do not fail it.
func (h *Hub) Probe(ctx context.Context) error {
	if h == nil || h.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "WebSocket probe requires a hub and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closing || h.terminal != nil:
		return Stopping
	case !h.clusterReadyLocked():
		return Unavailable
	}
	return nil
}
