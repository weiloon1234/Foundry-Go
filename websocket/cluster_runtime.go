package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

type clusterState struct {
	backend  ClusterBackend
	config   ClusterConfig
	key      ClusterKey
	instance InstanceID
	topic    pubsub.Channel
	// Lifecycle fields below are protected by Hub.mu.
	started  bool
	ready    chan struct{}
	startErr error
}

func NewDistributed(registry *Registry, authentication *foundryhttp.Authentication, config Config, backend ClusterBackend, cluster ClusterConfig) (*Hub, error) {
	hub, err := New(registry, authentication, config)
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
func NewPublisher(registry *Registry, config Config, backend ClusterBackend, cluster ClusterConfig) (*Publisher, error) {
	hub, err := newHub(registry, nil, config, false)
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
	h.cluster = &clusterState{backend: backend, config: config, key: key, topic: topic, instance: instance, ready: make(chan struct{})}
	return nil
}

// Start confirms the distributed subscription before upgrades may be accepted.
// A local Hub needs no separate startup. The wait context bounds waiting; Stop
// owns any setup still in progress. Failed/gapped cluster instances cannot restart.
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
func (h *Hub) clusterCall(ctx context.Context, operation func(context.Context) error) error {
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, h.cluster.config.OperationTimeout)
	defer cancel()
	ctx, finish := ownedContext(ctx, h)
	defer finish()
	ordinaryDenial := false
	err := callback.Isolated("WebSocket cluster operation", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(ctx); err != nil {
			ordinaryDenial = errorgraph.Is(err, CapacityExceeded) || errorgraph.Is(err, Stopping)
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		if !ordinaryDenial && caller.Err() == nil {
			h.clusterFailed(err)
		}
	} else {
		h.mu.Lock()
		if !h.closing {
			h.degraded = false
		}
		h.mu.Unlock()
	}
	return err
}
func (h *Hub) clusterFailed(err error) {
	h.mu.Lock()
	h.degraded = true
	h.failures++
	running := h.cluster != nil && h.cluster.started
	if running && h.terminal == nil {
		h.terminal = err
	}
	h.mu.Unlock()
	if running {
		h.beginStop()
	}
}
func (h *Hub) clusterLoop() {
	state := h.cluster
	var stream pubsub.Stream
	var rawDone <-chan struct{}
	ctx, finish := ownedContext(h.ctx, h)
	defer finish()
	defer func() {
		if stream != nil {
			cleanupContext, cleanupFinished := ownedContext(context.Background(), h)
			defer cleanupFinished()
			cleanup := callback.Isolated("WebSocket cluster cleanup", func() error { return stream.Close(cleanupContext) })
			if rawDone != nil {
				<-rawDone
			}
			if cleanup != nil {
				h.clusterFailed(cleanup)
			}
		}
		h.mu.Lock()
		h.background--
		h.completeLocked()
		h.mu.Unlock()
	}()
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
	h.mu.Lock()
	state.startErr = err
	close(state.ready)
	h.mu.Unlock()
	if err != nil {
		return
	}
	for {
		err = callback.Isolated("WebSocket cluster receive", func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			message, err := stream.Next(ctx)
			if err != nil {
				return err
			}
			if message.Channel != state.topic || len(message.Data) > state.config.Buffer.PayloadBytes {
				return fault.New(fault.Invalid, "cluster delivered an invalid transport frame")
			}
			return h.receiveCluster(ctx, message.Data)
		})
		if err != nil {
			if ctx.Err() == nil {
				h.clusterFailed(errors.Join(pubsub.ErrDisconnected, err))
			}
			return
		}
	}
}

func (h *Hub) clusterReadyLocked() bool {
	if h.cluster == nil {
		return true
	}
	if h.degraded || !h.cluster.started {
		return false
	}
	select {
	case <-h.cluster.ready:
		return h.cluster.startErr == nil
	default:
		return false
	}
}
