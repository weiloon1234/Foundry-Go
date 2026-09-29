package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// PolicyConflict reports live cluster state written under a different channel
// registry or authority limits in the same namespace. It is the unrecoverable
// configuration fault that terminates a running hub; adapters return it (or
// wrap it) for such state. Other operation failures are treated as transient.
var PolicyConflict = fault.New(fault.Conflict, "live WebSocket cluster policy differs")

// MembershipConflict reports that the authority still holds a different
// membership of this connection for the scope, typically left behind by a
// leave that failed transiently. The hub releases that record and joins again;
// it is a per-operation failure, never a namespace policy conflict.
var MembershipConflict = fault.New(fault.Conflict, "WebSocket connection holds a different membership for this scope")

// Resubscription backoff after a lost fan-out stream.
const (
	resubscribeInitial = 100 * time.Millisecond
	resubscribeMaximum = 5 * time.Second
)

type clusterState struct {
	backend  ClusterBackend
	config   ClusterConfig
	key      ClusterKey
	instance InstanceID
	topic    pubsub.Channel
	// Lifecycle fields below are protected by Hub.mu.
	started   bool
	ready     chan struct{}
	startErr  error
	streaming bool
	// dirty holds active presence scopes awaiting an authority refresh by the
	// presence worker; wake signals it without blocking the receive loop.
	dirty map[subscriptionKey]struct{}
	wake  chan struct{}
	// exclusions keeps live-delivery exclusions of this instance's own
	// connections for publications awaiting their fan-out echo, so the
	// envelope itself never carries them (see ExcludeRemoteConnections).
	exclusions     map[MessageID]ConnectionID
	exclusionOrder []pendingExclusion
}

// pendingExclusion expires an exclusion whose echo was lost with its stream.
type pendingExclusion struct {
	id      MessageID
	expires time.Time
}

// Local exclusions are bounded in count and lifetime.
const (
	maxLocalExclusions = 4096
	exclusionLifetime  = time.Minute
)

func NewDistributed(registry *Registry, authentication *foundryhttp.Authentication, config Config, backend ClusterBackend, cluster ClusterConfig, opts ...Option) (*Hub, error) {
	hub, err := New(registry, authentication, config, opts...)
	if err != nil {
		return nil, err
	}
	if err := hub.configureCluster(backend, cluster); err != nil {
		hub.cancel()
		return nil, err
	}
	return hub, nil
}

// NewPublisher constructs trusted server-side publication without authentication
// scopes or socket ownership. Use the exact same channel and cluster policies as
// the socket servers. Stop publication before closing the borrowed Redis client.
func NewPublisher(registry *Registry, config Config, backend ClusterBackend, cluster ClusterConfig, opts ...Option) (*Publisher, error) {
	hub, err := newHub(registry, nil, config, false, opts)
	if err != nil {
		return nil, err
	}
	if err := hub.configureCluster(backend, cluster); err != nil {
		hub.cancel()
		return nil, err
	}
	return &Publisher{hub: hub}, nil
}
func (h *Hub) configureCluster(backend ClusterBackend, config ClusterConfig) error {
	if backend == nil {
		return fault.New(fault.Invalid, "WebSocket cluster requires an adapter")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if config.Buffer.PayloadBytes < base64.StdEncoding.EncodedLen(h.config.MaxFrameBytes)+2048 {
		return fault.New(fault.Invalid, "cluster fan-out buffer cannot hold its encoded frame")
	}
	retention := 2 * config.ConnectionTTL
	for _, channel := range h.registry.ordered {
		retention = max(retention, channel.replay.TTL)
	}
	limits := ClusterLimits{Connections: config.MaxConnections, ConnectionsPerSubject: config.MaxConnectionsPerSubject, Subscriptions: h.config.MaxSubscriptions, PresenceMembers: h.config.MaxPresenceMembers, MemberBytes: h.config.MaxMemberBytes, FrameBytes: h.config.MaxFrameBytes, ConnectionTTL: config.ConnectionTTL, Retention: retention}
	metadata, err := json.Marshal(struct {
		Channels []ChannelInfo
		Limits   ClusterLimits
	}{h.registry.Channels(), limits})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(metadata)
	key, err := NewClusterKey(config.Namespace, hex.EncodeToString(sum[:]), limits)
	if err != nil {
		return err
	}
	topic, err := pubsub.NewChannel(config.Namespace, "realtime", 1, "fanout")
	if err != nil {
		return err
	}
	instance, err := model.NewID[Instance]()
	if err != nil {
		return err
	}
	h.clusterPresence = make(map[subscriptionKey]PresenceSnapshot)
	h.cluster = &clusterState{backend: backend, config: config, key: key, topic: topic, instance: instance, ready: make(chan struct{}), dirty: make(map[subscriptionKey]struct{}), wake: make(chan struct{}, 1), exclusions: make(map[MessageID]ConnectionID)}
	return nil
}

// Start confirms the distributed subscription before upgrades may be accepted.
// A local Hub needs no separate startup. The wait context bounds waiting; Stop
// owns any setup still in progress. A failed initial subscription is terminal;
// later stream loss is recovered by resubscribing without stopping the hub.
func (h *Hub) Start(ctx context.Context) error {
	if h == nil || h.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "WebSocket start requires a hub and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		return Stopping
	}
	state := h.cluster
	if state == nil {
		h.mu.Unlock()
		return nil
	}
	if !state.started {
		state.started = true
		h.background++
		go h.clusterLoop()
	}
	h.mu.Unlock()
	select {
	case <-state.ready:
		h.mu.Lock()
		err := state.startErr
		stopping := h.closing
		h.mu.Unlock()
		if err != nil {
			return err
		}
		if stopping {
			return Stopping
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// clusterError carries the classification of a failed cluster operation ahead
// of its cause, so callers inspect a framework code without running adapter
// error methods. Transient failures carry Unavailable, terminal ones Stopping.
type clusterError struct {
	code  Code
	cause error
}

func (e *clusterError) Error() string   { return string(e.code) }
func (e *clusterError) Unwrap() []error { return []error{e.code, e.cause} }

func clusterCode(err error) Code {
	if failure, ok := err.(*clusterError); ok {
		return failure.code
	}
	if code, ok := err.(Code); ok {
		return code
	}
	return ""
}

// clusterCall runs one bounded adapter operation. Ordinary denials (capacity,
// absent/expired ownership) return their protocol code. Any other failure fails
// only this operation with a retryable Unavailable code and marks the hub
// degraded until an operation succeeds; a policy conflict is terminal.
func (h *Hub) clusterCall(ctx context.Context, operation func(context.Context) error) error {
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, h.cluster.config.OperationTimeout)
	defer cancel()
	ctx, finish := ownedContext(ctx, h)
	defer finish()
	var denial Code
	terminal := false
	err := callback.Isolated("WebSocket cluster operation", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(ctx); err != nil {
			// One bounded walk classifies arbitrary adapter errors.
			errorgraph.Walk(err, func(current error) bool {
				switch {
				case errorgraph.Matches(current, PolicyConflict):
					terminal = true
				case errorgraph.Matches(current, CapacityExceeded):
					denial = CapacityExceeded
				case errorgraph.Matches(current, Stopping):
					denial = Stopping
				}
				return !terminal
			})
			if terminal {
				denial = ""
			}
			return err
		}
		return ctx.Err()
	})
	switch {
	case err == nil:
		h.clusterRecovered()
		return nil
	case denial != "":
		return denial
	case caller.Err() != nil:
		return err
	case terminal:
		h.clusterTerminated(err)
		return &clusterError{code: Stopping, cause: err}
	default:
		h.clusterDegraded(err)
		return &clusterError{code: Unavailable, cause: err}
	}
}
func (h *Hub) clusterDegraded(err error) {
	h.counters.failures.Add(1)
	h.mu.Lock()
	transition := !h.degraded && !h.closing
	h.degraded = true
	h.mu.Unlock()
	if transition {
		h.logFailure(slog.LevelWarn, "WebSocket cluster degraded", err)
	}
}
func (h *Hub) clusterRecovered() {
	h.mu.Lock()
	// A publisher never subscribes; a socket hub recovers only with its stream.
	transition := h.degraded && !h.closing && (h.cluster.streaming || !h.cluster.started)
	if transition {
		h.degraded = false
	}
	h.mu.Unlock()
	if transition {
		h.log(slog.LevelInfo, "WebSocket cluster recovered")
	}
}

// clusterTerminated records an unrecoverable fault and stops the hub. Stop and
// the kernel report it; transient Redis failures never reach this path.
func (h *Hub) clusterTerminated(err error) {
	h.counters.failures.Add(1)
	h.mu.Lock()
	h.degraded = true
	first := h.terminal == nil
	if first {
		h.terminal = err
	}
	running := h.cluster != nil && h.cluster.started
	h.mu.Unlock()
	if first {
		h.logFailure(slog.LevelError, "WebSocket cluster terminated", err)
	}
	if running {
		h.beginStop()
	}
}
func (h *Hub) log(level slog.Level, message string, attrs ...slog.Attr) {
	if logger := h.logger.Load(); logger != nil {
		logger.LogAttrs(context.Background(), level, message, attrs...)
	}
}

// logFailure describes err only when a logger is configured; the bounded,
// redacted diagnostic never formats arbitrary error text.
func (h *Hub) logFailure(level slog.Level, message string, err error, attrs ...slog.Attr) {
	if logger := h.logger.Load(); logger != nil {
		logger.LogAttrs(context.Background(), level, message, append(attrs, slog.Any("diagnostic", errordiag.Describe(err)))...)
	}
}

// clusterLoop owns the fan-out subscription. Loss of the stream (buffer
// overflow, disconnect or adapter failure) is recovered by resubscribing with
// jittered backoff; connections that may have missed messages are closed with a
// retryable status unless RetainConnectionsOnGap is set, and presence scopes are
// resynchronized from the authority.
func (h *Hub) clusterLoop() { h.superviseCluster(false) }

// superviseCluster runs the receive loop. The adapter's Stream.Next runs on
// this goroutine; if it calls runtime.Goexit, the deferred recovery treats the
// stream as lost and continues supervision on a new goroutine, so the hub
// never keeps reporting a stream nobody reads.
func (h *Hub) superviseCluster(recovering bool) {
	state := h.cluster
	var stream pubsub.Stream
	var rawDone <-chan struct{}
	ctx, finish := ownedContext(h.ctx, h)
	defer finish()
	returned := false
	defer func() {
		if stream != nil {
			if err := h.closeStream(stream, rawDone); err != nil {
				if returned {
					h.clusterTerminated(err)
				} else {
					h.counters.cleanupFailures.Add(1)
				}
			}
		}
		relaunch := false
		if !returned && ctx.Err() == nil && h.terminalError() == nil {
			h.streamLost(fault.New(fault.Panicked, "WebSocket cluster receive exited abnormally"))
			h.mu.Lock()
			if relaunch = !h.closing; relaunch {
				h.background++
			}
			h.mu.Unlock()
		}
		if relaunch {
			go h.superviseCluster(true)
		}
		h.mu.Lock()
		h.background--
		h.completeLocked()
		h.mu.Unlock()
	}()
	var err error
	if recovering {
		if stream, rawDone = h.resubscribeCluster(ctx); stream == nil {
			returned = true
			return
		}
		h.streamRestored()
	} else {
		stream, rawDone, err = h.subscribeCluster(ctx)
		h.mu.Lock()
		state.startErr = err
		state.streaming = err == nil
		close(state.ready)
		h.mu.Unlock()
		if err != nil {
			h.clusterTerminated(err)
			returned = true
			return
		}
		h.startPresenceWorker(ctx)
	}
	for {
		err = h.receiveStream(ctx, stream)
		if ctx.Err() != nil {
			returned = true
			return
		}
		closing := stream
		stream = nil
		if cleanup := h.closeStream(closing, rawDone); cleanup != nil {
			h.counters.cleanupFailures.Add(1)
		}
		h.streamLost(err)
		if stream, rawDone = h.resubscribeCluster(ctx); stream == nil {
			returned = true
			return
		}
		h.streamRestored()
	}
}

// resubscribeCluster retries the subscription with jittered exponential
// backoff. It returns nil when shutdown or a terminal fault stopped the hub.
func (h *Hub) resubscribeCluster(ctx context.Context) (pubsub.Stream, <-chan struct{}) {
	backoff := resubscribeInitial
	for {
		// Full jitter keeps a fleet from resubscribing in lockstep.
		timer := time.NewTimer(backoff/2 + rand.N(backoff/2+1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil
		case <-timer.C:
		}
		backoff = min(2*backoff, resubscribeMaximum)
		stream, rawDone, err := h.subscribeCluster(ctx)
		if err == nil {
			return stream, rawDone
		}
		if ctx.Err() != nil || h.terminalError() != nil {
			return nil, nil // Shutdown or a terminal fault already stopped the hub.
		}
	}
}

func (h *Hub) subscribeCluster(ctx context.Context) (pubsub.Stream, <-chan struct{}, error) {
	state := h.cluster
	var stream pubsub.Stream
	var rawDone <-chan struct{}
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		if err := state.backend.WebSocketCheck(ctx, state.key); err != nil {
			return err
		}
		var err error
		stream, err = state.backend.Subscribe(ctx, []pubsub.Channel{state.topic}, state.config.Buffer)
		if err != nil {
			return err
		}
		if stream == nil {
			return fault.New(fault.Invalid, "cluster adapter returned no subscription")
		}
		rawDone = stream.Done()
		if rawDone == nil {
			return fault.New(fault.Invalid, "cluster subscription has no ownership completion")
		}
		return nil
	})
	if err != nil && stream != nil {
		if cleanup := h.closeStream(stream, rawDone); cleanup != nil {
			h.counters.cleanupFailures.Add(1)
		}
		stream = nil
	}
	return stream, rawDone, err
}

// closeStream releases an owned subscription and waits for adapter work.
func (h *Hub) closeStream(stream pubsub.Stream, rawDone <-chan struct{}) error {
	cleanupContext, cleanupFinished := ownedContext(context.Background(), h)
	defer cleanupFinished()
	err := callback.Isolated("WebSocket cluster cleanup", func() error { return stream.Close(cleanupContext) })
	if rawDone != nil {
		<-rawDone
	}
	return err
}

// receiveStream decodes and routes each envelope once on the receive loop.
// Presence changes only mark scopes for the presence worker, so a burst never
// waits on authority round trips. Invalid envelopes are dropped and counted.
func (h *Hub) receiveStream(ctx context.Context, stream pubsub.Stream) error {
	topic, maximum := h.cluster.topic, h.cluster.config.Buffer.PayloadBytes
	for {
		var message pubsub.Message
		// A panic in the adapter ends this stream like any other stream error.
		if err := callback.Invoke("WebSocket cluster receive", func() error {
			var err error
			message, err = stream.Next(ctx)
			return err
		}); err != nil {
			return err
		}
		failed := callback.Invoke("WebSocket cluster envelope", func() error {
			if message.Channel != topic || len(message.Data) > maximum {
				return fault.New(fault.Invalid, "cluster delivered an invalid transport frame")
			}
			return h.receiveCluster(ctx, message.Data)
		})
		if err := ctx.Err(); err != nil {
			return err
		}
		if failed != nil {
			if dropped := h.counters.droppedEnvelopes.Add(1); dropped == 1 {
				h.logFailure(slog.LevelWarn, "WebSocket cluster envelope dropped", failed)
			}
		}
	}
}

func (h *Hub) streamLost(cause error) {
	h.counters.gaps.Add(1)
	h.counters.failures.Add(1)
	h.mu.Lock()
	h.cluster.streaming = false
	h.degraded = true
	retain := h.cluster.config.RetainConnectionsOnGap
	closed := 0
	if !retain && !h.closing {
		// Live delivery may have been lost. Retryable closure lets clients
		// reconnect and request recent replay instead of silently missing events.
		for _, connection := range h.connections {
			connection.startDrainLocked(closeTryAgainLater)
			connection.operationCancel()
			closed++
		}
	}
	h.mu.Unlock()
	h.logFailure(slog.LevelWarn, "WebSocket cluster stream lost", cause, slog.Int("closed_connections", closed))
}
func (h *Hub) streamRestored() {
	h.counters.resubscriptions.Add(1)
	h.mu.Lock()
	h.cluster.streaming = true
	h.degraded = false
	// Presence changes published during the gap were missed: refresh every
	// active scope from the authority. Live memberships renew by heartbeat.
	for key := range h.clusterPresence {
		h.cluster.dirty[key] = struct{}{}
	}
	h.mu.Unlock()
	h.wakePresence()
	h.log(slog.LevelInfo, "WebSocket cluster stream restored")
}

func (h *Hub) clusterReadyLocked() bool {
	if h.cluster == nil {
		return true
	}
	if !h.cluster.started || !h.cluster.streaming {
		return false
	}
	select {
	case <-h.cluster.ready:
		return h.cluster.startErr == nil
	default:
		return false
	}
}
