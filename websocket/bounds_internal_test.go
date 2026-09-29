package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

func TestSlowConsumerDisconnectsWithoutBlockingPublisher(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("bounds", DefineRooms(foundryhttp.IntegerPath[int64]()))
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(r, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop(context.Background())
	id, err := model.NewID[Connection]()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	c := &connectionState{hub: h, id: id, ctx: ctx, cancel: cancel, outbound: make(chan []byte, 1)}
	h.mu.Lock()
	first := c.enqueueLocked([]byte("first"))
	second := c.enqueueLocked([]byte("second"))
	h.mu.Unlock()
	if !first || second || ctx.Err() == nil || h.Snapshot().SlowConsumers != 1 {
		t.Fatal("slow peer did not release transport promptly")
	}
}

func TestConfigurationBoundsAggregateQueuesAndPresenceFrames(t *testing.T) {
	for _, mutate := range []func(*Config){func(c *Config) { c.InboundQueue = 0 }, func(c *Config) { c.MaxQueuedBytes = c.MaxFrameBytes - 1 }, func(c *Config) { c.MaxTotalQueuedBytes = int64(c.MaxQueuedBytes) - 1 }, func(c *Config) { c.MaxOperations = 0 }, func(c *Config) { c.MaxPresenceMembers = 4096 }, func(c *Config) { c.Payload.Bytes = c.MaxFrameBytes }, func(c *Config) { c.AdditionalOrigins = []foundryhttp.Origin{"null"} }, func(c *Config) {
		c.AdditionalOrigins = []foundryhttp.Origin{"https://a.example", "https://A.example:443"}
	}} {
		c := DefaultConfig()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("invalid resource/origin policy accepted")
		}
	}
}

func TestQueuedByteBudgetsDisconnectBeforeRetainingData(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("budget", DefineRooms(foundryhttp.IntegerPath[int64]()))
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.MaxConnections = 65536 // Actual bytes, not a worst-case product, bound memory.
	config.MaxQueuedBytes = config.MaxFrameBytes
	config.MaxTotalQueuedBytes = int64(config.MaxFrameBytes) + 8
	h, err := New(r, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop(context.Background())
	connection := func() (*connectionState, context.Context) {
		id, err := model.NewID[Connection]()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(h.ctx)
		t.Cleanup(cancel)
		return &connectionState{hub: h, id: id, ctx: ctx, cancel: cancel, outbound: make(chan []byte, 64)}, ctx
	}
	first, firstCtx := connection()
	frame := make([]byte, config.MaxFrameBytes-8)
	h.mu.Lock()
	admitted := first.enqueueLocked(frame)
	over := first.enqueueLocked(make([]byte, 16))
	h.mu.Unlock()
	if !admitted || over || firstCtx.Err() == nil {
		t.Fatal("per-connection byte budget was not enforced")
	}
	second, secondCtx := connection()
	h.mu.Lock()
	shared := second.enqueueLocked(make([]byte, 32))
	h.mu.Unlock()
	if shared || secondCtx.Err() == nil || h.Snapshot().QueuedBytes != int64(len(frame)) {
		t.Fatal("hub byte budget was not enforced", h.Snapshot().QueuedBytes)
	}
	first.releaseQueues()
	if h.Snapshot().QueuedBytes != 0 {
		t.Fatal("released queues retained budget")
	}
}

func TestTokenBucketUsesMonotonicTime(t *testing.T) {
	now := time.Now()
	bucket := newTokenBucket(ratelimit.Limit{Requests: 2, Window: time.Second}, now)
	if !bucket.allow(now) || !bucket.allow(now) || bucket.allow(now) {
		t.Fatal("burst not bounded")
	}
	// A wall-clock step backwards is invisible to monotonic arithmetic.
	stepped := now.Add(-time.Hour)
	if bucket.allow(stepped) {
		t.Fatal("backward step refilled or corrupted the bucket")
	}
	if !bucket.allow(now.Add(600 * time.Millisecond)) {
		t.Fatal("bucket did not refill over elapsed time")
	}
}

func TestManagementOperationsQueueThenReportOverload(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("operations", DefineRooms(foundryhttp.IntegerPath[int64]()))
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.MaxOperations = 1
	config.OperationTimeout = 30 * time.Millisecond
	h, err := New(r, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop(context.Background())
	_, finish, err := h.operation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, _, err := h.operation(t.Context()); !errors.Is(err, fault.Overloaded) || time.Since(started) < 20*time.Millisecond {
		t.Fatal("exhausted capacity must wait briefly, then report overload", err)
	}
	waiter := make(chan error, 1)
	go func() {
		_, done, err := h.operation(context.Background())
		if err == nil {
			done()
		}
		waiter <- err
	}()
	time.Sleep(5 * time.Millisecond)
	finish()
	if err := <-waiter; err != nil {
		t.Fatal("queued operation was not admitted after release", err)
	}
	if h.Snapshot().OperationOverloads != 1 || h.Snapshot().ActiveOperations != 0 {
		t.Fatal("operation accounting drifted", h.Snapshot())
	}
	if operationCode(errors.Join(fault.New(fault.Overloaded, "busy"), context.DeadlineExceeded)) != Unavailable {
		t.Fatal("overload must map to the retryable wire code")
	}
}

// An exhausted hub budget disconnects the connections holding it, not a
// reading subscriber that happened to receive the next frame.
func TestHubBudgetEvictsLargestQueueHolder(t *testing.T) {
	type owner struct{}
	channel := Public[owner]("fair", DefineRooms(foundryhttp.IntegerPath[int64]()))
	r, err := NewRegistry(Register(channel))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.MaxQueuedBytes = config.MaxFrameBytes
	config.MaxTotalQueuedBytes = int64(config.MaxFrameBytes) + 8
	h, err := New(r, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// These states have no loops; unregister them so Stop can complete.
		h.mu.Lock()
		clear(h.connections)
		h.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	connection := func() (*connectionState, context.Context) {
		id, err := model.NewID[Connection]()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(h.ctx)
		t.Cleanup(cancel)
		c := &connectionState{hub: h, id: id, ctx: ctx, cancel: cancel, outbound: make(chan []byte, 64), inbound: make(chan []byte, 1)}
		h.mu.Lock()
		h.connections[id] = c
		h.mu.Unlock()
		return c, ctx
	}
	slow, slowCtx := connection()
	reader, readerCtx := connection()
	h.mu.Lock()
	held := slow.enqueueLocked(make([]byte, config.MaxFrameBytes-8))
	delivered := reader.enqueueLocked(make([]byte, 32))
	h.mu.Unlock()
	if !held || !delivered || slowCtx.Err() == nil || readerCtx.Err() != nil {
		t.Fatal("the largest holder was not the one disconnected")
	}
	if h.Snapshot().QueuedBytes != 32 || h.Snapshot().SlowConsumers != 1 {
		t.Fatal("evicted queue kept its budget", h.Snapshot().QueuedBytes)
	}
	// When the current connection holds the most, it is the slow consumer.
	h.mu.Lock()
	grown := reader.enqueueLocked(make([]byte, config.MaxFrameBytes-40))
	h.mu.Unlock()
	if !grown {
		t.Fatal("reader within budget was refused")
	}
	other, otherCtx := connection()
	h.mu.Lock()
	h.connections[other.id] = other
	overflow := reader.enqueueLocked(make([]byte, 64))
	h.mu.Unlock()
	if overflow || readerCtx.Err() == nil || otherCtx.Err() != nil {
		t.Fatal("the largest holder must disconnect itself rather than an idle peer")
	}
}

func TestRateLimitedReplyRecoversOnlyALeadingRequestID(t *testing.T) {
	for frame, want := range map[string]RequestID{
		`{"v":1,"action":"message","id":"r7","channel":"chat","payload":{"x":1}}`: "r7",
		`{"id":"first","v":1}`:                                        "first",
		`{"v":1,"payload":{"id":"nested"},"id":"late"}`:               "",
		`{"v":1,"action":"a","channel":"c","event":"e","id":"fifth"}`: "",
		`{"id":"Not Semantic"}`:                                       "",
		`not json`:                                                    "",
	} {
		if got := leadingRequestID([]byte(frame)); got != want {
			t.Fatal("leading request ID", frame, got)
		}
	}
}
