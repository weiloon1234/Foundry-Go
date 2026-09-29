package websocket

import (
	"context"
	stdhttp "net/http"
	"strings"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func (h *Hub) originAllowed(r *stdhttp.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return h.config.AllowOriginless
	}
	if len(values) != 1 {
		return false
	}
	origin, err := foundryhttp.ParseOrigin(values[0])
	if err != nil || origin == foundryhttp.NullOrigin {
		return false
	}
	target, err := foundryhttp.RequestOrigin(r)
	if err != nil {
		return false
	}
	if origin == target {
		return true
	}
	for _, raw := range h.config.AdditionalOrigins {
		allowed, _ := foundryhttp.ParseOrigin(string(raw))
		if origin == allowed {
			return true
		}
	}
	return false
}
func hasSubprotocol(r *stdhttp.Request) bool {
	bytes := 0
	for _, field := range r.Header.Values("Sec-WebSocket-Protocol") {
		bytes += len(field)
		if bytes > 1024 {
			return false
		}
	}
	for _, field := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, item := range strings.Split(field, ",") {
			if strings.TrimSpace(item) == Subprotocol {
				return true
			}
		}
	}
	return false
}
func rejectUpgrade(w stdhttp.ResponseWriter, r *stdhttp.Request, code foundryhttp.ErrorCode) {
	_ = foundryhttp.WriteError(w, r, code)
}

// ServeHTTP remains active until all socket loops and callbacks actually exit.
// Native HTTP middleware can wrap this handler. Route only the intended upgrade
// path to it. A manual host must call Stop; Module owns both listener and Hub.
func (h *Hub) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if h == nil || h.done == nil {
		rejectUpgrade(w, r, foundryhttp.Unavailable)
		return
	}
	recorder := observability.FromContext(r.Context())
	gate := maintenance.FromContext(r.Context())
	if gate == nil {
		// Manual compositions may carry only a recorder and its gate.
		gate = recorder.Gate()
	}
	if gate.Admit() != nil {
		h.counters.rejected.Add(1)
		rejectUpgrade(w, r, foundryhttp.Unavailable)
		return
	}
	if r.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", "GET")
		rejectUpgrade(w, r, foundryhttp.MethodNotAllowed)
		return
	}
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.User != nil || !hasSubprotocol(r) {
		rejectUpgrade(w, r, foundryhttp.BadRequest)
		return
	}
	if !h.originAllowed(r) {
		rejectUpgrade(w, r, foundryhttp.Forbidden)
		return
	}
	var credentials auth.Credentials
	if h.authentication != nil {
		var err error
		credentials, err = h.authentication.CaptureCredentials(r)
		if err != nil {
			rejectUpgrade(w, r, foundryhttp.Unauthenticated)
			return
		}
	}
	id, err := model.NewID[Connection]()
	if err != nil {
		rejectUpgrade(w, r, foundryhttp.Unavailable)
		return
	}
	metadata := attribution.FromContext(r.Context()).Request()
	if !metadata.IP.IsValid() {
		metadata.IP = foundryhttp.PeerIP(r)
	}
	origin, err := (attribution.Origin{}).WithRequest(metadata)
	if err != nil {
		rejectUpgrade(w, r, foundryhttp.BadRequest)
		return
	}
	base, err := attribution.WithContext(h.ctx, origin)
	if err != nil {
		rejectUpgrade(w, r, foundryhttp.BadRequest)
		return
	}
	// Preserve only declared correlation/runtime capabilities across the HTTP
	// boundary. Socket lifetime still belongs to the Hub, not the HTTP deadline.
	base = maintenance.WithContext(observability.WithContext(base, recorder), gate)
	if parent := tracing.FromContext(r.Context()); !parent.IsZero() {
		base, _ = tracing.WithContext(base, parent)
	}
	observed, span, _ := recorder.Start(base, observability.Operation{Kind: observability.Socket, Name: "connection"})
	if observed != nil {
		base = observed
	}
	outcome := observability.Rejected
	defer func() { span.End(observability.Result{Outcome: outcome}) }()
	ctx, operationCancel := context.WithCancel(base)
	transportContext, transportCancel := context.WithCancel(context.Background())
	cancel := func() { operationCancel(); transportCancel() }
	c := &connectionState{hub: h, id: id, ip: metadata.IP, ctx: ctx, cancel: cancel, operationCancel: operationCancel, transportContext: transportContext, credentials: credentials, subscriptions: make(map[subscriptionKey]*subscriptionState), inbound: make(chan []byte, h.config.InboundQueue), outbound: make(chan []byte, h.config.OutboundQueue)}
	h.mu.Lock()
	if h.closing || !h.clusterReadyLocked() || len(h.connections) >= h.config.MaxConnections || h.ipConnections[c.ip] >= h.config.MaxConnectionsPerIP {
		h.counters.rejected.Add(1)
		h.mu.Unlock()
		cancel()
		rejectUpgrade(w, r, foundryhttp.Unavailable)
		return
	}
	h.connections[id] = c
	h.ipConnections[c.ip]++
	h.mu.Unlock()
	defer func() {
		cancel()
		c.cleanup()
		h.mu.Lock()
		delete(h.connections, id)
		h.ipConnections[c.ip]--
		if h.ipConnections[c.ip] == 0 {
			delete(h.ipConnections, c.ip)
		}
		h.mu.Unlock()
		// No producer can reach the connection once it left every index.
		c.releaseQueues()
		h.mu.Lock()
		h.completeLocked()
		h.mu.Unlock()
	}()
	if ctx.Err() != nil {
		rejectUpgrade(w, r, foundryhttp.Unavailable)
		return
	}
	if h.cluster != nil {
		c.clusterOpened = true
		if err := h.clusterCall(ctx, func(ctx context.Context) error {
			return h.cluster.backend.WebSocketOpen(ctx, h.cluster.key, h.cluster.instance, c.id)
		}); err != nil {
			// Transient cluster failures reject only this upgrade (retryable 503).
			h.counters.rejected.Add(1)
			rejectUpgrade(w, r, foundryhttp.Unavailable)
			return
		}
	}
	// The exact scheme/authority policy above is stricter than the transport's
	// host-pattern policy. Disable that second policy only after ours passes.
	socket, err := transport.Accept(w, r, &transport.AcceptOptions{Subprotocols: []string{Subprotocol}, InsecureSkipVerify: true, CompressionMode: transport.CompressionDisabled})
	if err != nil {
		return
	}
	c.socket = socket
	defer socket.CloseNow()
	socket.SetReadLimit(int64(h.config.MaxFrameBytes))
	h.counters.accepted.Add(1)
	c.run()
	outcome = observability.Succeeded
	if h.ctx.Err() != nil {
		outcome = observability.Cancelled
	}
}
