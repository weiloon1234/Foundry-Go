package websockettest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

// Run requires real Redis pub/sub and atomic membership/history operations.
// Every case receives its own bounded namespace; no shared data is enumerated.
func Run(t *testing.T, b ws.ClusterBackend, namespace keyspace.Namespace) {
	config := func(suffix string) ws.ClusterConfig {
		n := namespace
		n.Environment += "-" + suffix
		return ws.DefaultClusterConfig(n)
	}
	t.Run("fanout_replay_and_relay", func(t *testing.T) {
		joined := make(chan ws.ConnectionID, 8)
		channel := ws.Public[RoomOwner]("chat", ws.DefineRooms(foundryhttp.IntegerPath[int64]())).WithReplay(ws.ReplayConfig{Messages: 3, Bytes: 4096, TTL: time.Second}).WithHooks(ws.Hooks[int64, ws.Anonymous]{Joined: func(_ context.Context, m ws.MessageContext[int64, ws.Anonymous]) error {
			joined <- m.Connection
			return nil
		}})
		outgoing := ws.DefineOutgoing(channel, "updated", textContract[Payload]())
		incoming := ws.DefineIncoming(channel, "relay", textContract[Payload]()).AcknowledgeAccepted()
		r := registry(t, ws.Register(channel, outgoing.Registration(), incoming.Relay(outgoing)))
		cluster := config("fanout")
		duplicate := duplicateBackend{b}
		first, second := serve(t, r, nil, duplicate, cluster), serve(t, r, nil, duplicate, cluster)
		publisher, err := ws.NewPublisher(r, localConfig(), duplicate, cluster)
		must(t, err)
		t.Cleanup(func() { must(t, publisher.Close(context.Background())) })
		a, c := first.dial(t, false), second.dial(t, false)
		zero := 0
		a.subscribe(t, "room-a", channel.ID(), room("1"), &zero)
		c.subscribe(t, "room-c", channel.ID(), room("2"), &zero)
		<-joined
		secondID := <-joined
		c.subscribe(t, "whole-c", channel.ID(), nil, &zero)
		id, err := ws.Publish(t.Context(), publisher, channel, int64(1), outgoing, Payload{"private-room"})
		must(t, err)
		if a.next(t, ws.EventResponse).MessageID != id {
			t.Fatal("cross-instance room delivery lost identity")
		}
		// A same-stream channel broadcast acts as a fan-out ordering barrier. If room
		// isolation or duplicate suppression fails, its ID will not be the next one.
		all, err := ws.Broadcast(t.Context(), publisher, channel, outgoing, Payload{"all"})
		must(t, err)
		for _, p := range []*peer{a, c} {
			if p.next(t, ws.EventResponse).MessageID != all {
				t.Fatal("room leak or duplicate publication")
			}
		}
		// The sender receives acceptance and completion separately from its event;
		// Redis delivery may race completion, so collect the latter two without order.
		a.send(t, ws.Request{Action: ws.Message, ID: "relay", Channel: channel.ID(), Room: room("1"), Event: "relay", Payload: payload("relayed")})
		a.next(t, ws.Accepted)
		seen := map[ws.ResponseType]bool{}
		for len(seen) < 2 {
			select {
			case frame := <-a.frames:
				if frame.Type != ws.Acknowledged && frame.Type != ws.EventResponse {
					t.Fatal("unexpected relay response", frame.Type)
				}
				if seen[frame.Type] {
					t.Fatal("relay duplicate")
				}
				seen[frame.Type] = true
			case <-time.After(3 * time.Second):
				t.Fatal("relay incomplete")
			}
		}
		a.close()
		eventually(t, func() bool { return first.hub.Snapshot().Connections == 0 })
		reconnect := first.dial(t, false)
		reconnect.subscribe(t, "recent", channel.ID(), room("1"), nil)
		ids := make(map[ws.MessageID]bool)
		for i := 0; i < 3; i++ {
			frame := reconnect.next(t, ws.EventResponse)
			if !frame.Replayed || ids[frame.MessageID] {
				t.Fatal("history marker/deduplication failed")
			}
			ids[frame.MessageID] = true
		}
		reconnect.barrier(t, channel.ID(), room("1"))
		c.barrier(t, channel.ID(), nil)
		c.barrier(t, channel.ID(), room("2"))
		must(t, ws.DisconnectConnection(t.Context(), publisher, secondID))
		eventually(t, func() bool { return second.hub.Snapshot().Connections == 0 })
		if first.hub.Snapshot().Connections != 1 {
			t.Fatal("connection disconnect crossed instance/identity")
		}
	})
	t.Run("presence_tabs_limits_and_revocation", func(t *testing.T) {
		adapter, guard := authenticated(t)
		channel := ws.Private[PresenceOwner]("members", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), guard, func(context.Context, Account, ws.Target[int64]) error { return nil })
		presence := ws.WithPresence(channel, textContract[Member](), func(context.Context, Account) (Member, error) { return Member{"safe"}, nil })
		r := registry(t, ws.Register(presence.Channel()))
		cluster := config("presence")
		cluster.MaxConnections = 3
		cluster.MaxConnectionsPerSubject = 2
		cluster.ConnectionTTL = 900 * time.Millisecond
		cluster.OperationTimeout = 200 * time.Millisecond
		first, second := serve(t, r, adapter, b, cluster), serve(t, r, adapter, b, cluster)
		a, c, excess := first.dial(t, true), second.dial(t, true), second.dial(t, true)
		a.subscribe(t, "one", channel.ID(), room("1"), nil)
		joined := c.subscribe(t, "two", channel.ID(), room("1"), nil)
		if len(joined.Members) != 1 || joined.Members[0].Connections != 2 || strings.Contains(string(joined.Members[0].Data), "never-export") {
			t.Fatal("cluster presence exposed model or miscounted tabs")
		}
		// A connection in multiple rooms consumes a single subject slot.
		c.subscribe(t, "second-room", channel.ID(), room("2"), nil)
		excess.send(t, ws.Request{Action: ws.Subscribe, ID: "over", Channel: channel.ID(), Room: room("1")})
		if excess.next(t, ws.ErrorResponse).Code != ws.CapacityExceeded {
			t.Fatal("distributed subject quota bypass")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		denied, err := client.Dial(ctx, "ws"+strings.TrimPrefix(first.http.URL, "http"), http.Header{"Origin": []string{first.http.URL}}, 64<<10)
		if denied != nil {
			denied.Close()
		}
		if err == nil {
			t.Fatal("cluster connection quota bypass")
		}
		if first.hub.Snapshot().Degraded || second.hub.Snapshot().Degraded {
			t.Fatal("ordinary capacity denial degraded authority")
		}
		a.close()
		eventually(t, func() bool {
			members, err := presence.Members(t.Context(), second.hub, ws.Room(int64(1)))
			return err == nil && len(members) == 1 && members[0].Connections == 1
		})
		// Surviving connections continuously touch their leases across multiple TTLs.
		time.Sleep(1100 * time.Millisecond)
		count, err := presence.Count(t.Context(), second.hub, ws.Room(int64(1)))
		must(t, err)
		if count != 1 {
			t.Fatal("live member expired")
		}
		publisher, err := ws.NewPublisher(r, localConfig(), b, cluster)
		must(t, err)
		defer publisher.Close(context.Background())
		must(t, ws.DisconnectSubject(t.Context(), publisher, guard, Account{ID: 7}.FoundryReference()))
		eventually(t, func() bool { return second.hub.Snapshot().Connections == 1 }) // Unsubscribed excess is not the subject.
		count, err = presence.Count(t.Context(), second.hub, ws.Room(int64(1)))
		must(t, err)
		if count != 0 {
			t.Fatal("last tab did not leave")
		}
	})
	t.Run("pending_replay_live_overlap", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("overlap", ws.DefineRooms(foundryhttp.IntegerPath[int64]())).WithReplay(ws.ReplayConfig{Messages: 4, Bytes: 4096, TTL: time.Minute})
		out := ws.DefineOutgoing(channel, "updated", textContract[Payload]())
		r := registry(t, ws.Register(channel, out.Registration()))
		blocked := &historyBarrier{ClusterBackend: b, entered: make(chan struct{}), release: make(chan struct{}), observed: make(chan struct{})}
		defer blocked.unblock()
		cluster := config("overlap")
		first := serve(t, r, nil, blocked, cluster)
		second := serve(t, r, nil, b, cluster)
		peer := first.dial(t, false)
		peer.send(t, ws.Request{Action: ws.Subscribe, ID: "join", Channel: channel.ID(), Room: room("1")})
		select {
		case <-blocked.entered:
		case <-time.After(time.Second):
			t.Fatal("history not requested")
		}
		id, err := ws.Publish(t.Context(), second.hub, channel, int64(1), out, Payload{"during-admission"})
		must(t, err)
		// Wait until the live bridge has buffered the frame, independently of history.
		select {
		case <-blocked.observed:
		case <-time.After(time.Second):
			t.Fatal("live bridge did not buffer publication")
		}
		blocked.unblock()
		peer.next(t, ws.Subscribed)
		frame := peer.next(t, ws.EventResponse)
		if frame.MessageID != id || !frame.Replayed {
			t.Fatal("replay/live overlap lost publication")
		}
		peer.barrier(t, channel.ID(), room("1"))
	})
	t.Run("transport_gap_resubscribes", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("gap", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
		out := ws.DefineOutgoing(channel, "updated", textContract[Payload]())
		gap := &gapBackend{ClusterBackend: b, fail: make(chan struct{})}
		first := serve(t, registry(t, ws.Register(channel, out.Registration())), nil, gap, config("gap"))
		peer := first.dial(t, false)
		peer.subscribe(t, "join", channel.ID(), room("1"), nil)
		close(gap.fail)
		// Sockets that may have missed events close with a retryable status;
		// the hub itself (and so the process) keeps running and resubscribes.
		eventually(t, func() bool { s := first.hub.Snapshot(); return s.Gaps == 1 && s.Connections == 0 })
		select {
		case <-peer.done:
		case <-time.After(3 * time.Second):
			t.Fatal("gapped socket stayed open")
		}
		eventually(t, func() bool {
			s := first.hub.Snapshot()
			return s.Streaming && !s.Degraded && !s.Stopping && s.Resubscriptions == 1
		})
		again := first.dial(t, false)
		again.subscribe(t, "rejoin", channel.ID(), room("1"), nil)
		id, err := ws.Publish(t.Context(), first.hub, channel, int64(1), out, Payload{"after-gap"})
		must(t, err)
		if again.next(t, ws.EventResponse).MessageID != id {
			t.Fatal("resubscribed stream did not deliver")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := first.hub.Stop(ctx); err != nil {
			t.Fatal("recovered gap reported as terminal", err)
		}
		if first.hub.Snapshot().BackgroundTasks != 0 {
			t.Fatal("bridge leaked after recovery")
		}
	})
	t.Run("transient_failure_fails_only_the_operation", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("transient", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
		flaky := &flakyJoinBackend{ClusterBackend: b}
		flaky.failures.Store(1)
		first := serve(t, registry(t, ws.Register(channel)), nil, flaky, config("transient"))
		peer := first.dial(t, false)
		peer.send(t, ws.Request{Action: ws.Subscribe, ID: "blip", Channel: channel.ID(), Room: room("1")})
		if code := peer.next(t, ws.ErrorResponse).Code; code != ws.Unavailable {
			t.Fatal("transient authority failure was not retryable", code)
		}
		if s := first.hub.Snapshot(); s.Stopping || s.Failures == 0 {
			t.Fatal("transient failure terminated or was not counted", s)
		}
		peer.subscribe(t, "retry", channel.ID(), room("1"), nil)
		if s := first.hub.Snapshot(); s.Degraded || s.Stopping {
			t.Fatal("successful retry did not clear degradation", s)
		}
	})
	t.Run("policy_conflict_is_terminal", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("conflict", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
		conflict := &flakyJoinBackend{ClusterBackend: b, conflict: true}
		conflict.failures.Store(1)
		first := serve(t, registry(t, ws.Register(channel)), nil, conflict, config("conflict"))
		peer := first.dial(t, false)
		peer.send(t, ws.Request{Action: ws.Subscribe, ID: "conflict", Channel: channel.ID(), Room: room("1")})
		eventually(t, func() bool { return first.hub.Snapshot().Stopping })
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := first.hub.Stop(ctx); !errors.Is(err, ws.PolicyConflict) {
			t.Fatal("policy conflict was not terminal", err)
		}
	})
	t.Run("backend_callback_cannot_wait_for_its_hub", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("cycle", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
		backend := &selfWaitBackend{ClusterBackend: b, result: make(chan error, 1)}
		server := serve(t, registry(t, ws.Register(channel)), nil, backend, config("cycle"))
		backend.hub = server.hub
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		peer, err := client.Dial(ctx, "ws"+strings.TrimPrefix(server.http.URL, "http"), http.Header{"Origin": []string{server.http.URL}}, 64<<10)
		if peer != nil {
			peer.Close()
		}
		if err == nil {
			t.Fatal("failed admission upgraded")
		}
		select {
		case err := <-backend.result:
			if !errors.Is(err, fault.Cycle) {
				t.Fatal("backend self-wait was not rejected", err)
			}
		case <-ctx.Done():
			t.Fatal("backend waited on its own connection")
		}
		// A failed operation rejects only its upgrade; the hub keeps running and
		// its owned work still drains on Stop.
		eventually(t, func() bool { return server.hub.Snapshot().Connections == 0 })
		if server.hub.Snapshot().Stopping {
			t.Fatal("per-operation failure stopped the hub")
		}
		if err := server.hub.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case <-server.hub.Done():
		case <-ctx.Done():
			t.Fatal("self-wait failure retained owned work")
		}
	})

	t.Run("stream_cleanup_cannot_wait_for_its_hub", func(t *testing.T) {
		channel := ws.Public[RoomOwner]("cleanup", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
		backend := &cleanupWaitBackend{ClusterBackend: b, result: make(chan error, 1)}
		server := serve(t, registry(t, ws.Register(channel)), nil, backend, config("cleanup"))
		backend.hub = server.hub
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := server.hub.Stop(ctx); !errors.Is(err, fault.Cycle) {
			t.Fatal("cleanup self-wait did not return cycle", err)
		}
		select {
		case err := <-backend.result:
			if !errors.Is(err, fault.Cycle) {
				t.Fatal("cleanup callback lacked owned context", err)
			}
		case <-ctx.Done():
			t.Fatal("cleanup waited for itself")
		}
		select {
		case <-server.hub.Done():
		case <-ctx.Done():
			t.Fatal("cleanup retained live bridge")
		}
	})

}

type selfWaitBackend struct {
	ws.ClusterBackend
	hub    *ws.Hub
	result chan error
}

func (b *selfWaitBackend) WebSocketOpen(ctx context.Context, k ws.ClusterKey, instance ws.InstanceID, id ws.ConnectionID) error {
	err := b.hub.Stop(ctx)
	b.result <- err
	return err
}
func (b *historyBarrier) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	stream, err := b.ClusterBackend.Subscribe(ctx, channels, limits)
	if err != nil {
		return nil, err
	}
	return &observedStream{Stream: stream, observed: b.observed}, nil
}

type observedStream struct {
	pubsub.Stream
	observed chan struct{}
	reads    int
}

func (s *observedStream) Next(ctx context.Context) (pubsub.Message, error) {
	s.reads++
	if s.reads == 2 {
		close(s.observed)
	}
	return s.Stream.Next(ctx)
}

type duplicateBackend struct{ ws.ClusterBackend }

func (b duplicateBackend) Publish(ctx context.Context, ch pubsub.Channel, data []byte) (uint64, error) {
	n, err := b.ClusterBackend.Publish(ctx, ch, data)
	if err != nil {
		return n, err
	}
	_, err = b.ClusterBackend.Publish(ctx, ch, data)
	return n, err
}

type historyBarrier struct {
	ws.ClusterBackend
	entered, release, observed chan struct{}
	once                       sync.Once
	enter                      sync.Once
}

func (b *historyBarrier) unblock() { b.once.Do(func() { close(b.release) }) }
func (b *historyBarrier) WebSocketHistory(ctx context.Context, k ws.ClusterKey, ch ws.ChannelID, p ws.ReplayConfig) ([][]byte, error) {
	b.enter.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return b.ClusterBackend.WebSocketHistory(ctx, k, ch, p)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// gapBackend interrupts only its first stream, like a transient disconnect.
type gapBackend struct {
	ws.ClusterBackend
	fail          chan struct{}
	subscriptions atomic.Int32
}

func (b *gapBackend) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	s, err := b.ClusterBackend.Subscribe(ctx, channels, limits)
	if err != nil || b.subscriptions.Add(1) > 1 {
		return s, err
	}
	return &gapStream{Stream: s, fail: b.fail}, nil
}

// flakyJoinBackend fails a bounded number of joins with a transient error or a
// policy conflict, then behaves normally.
type flakyJoinBackend struct {
	ws.ClusterBackend
	failures atomic.Int32
	conflict bool
}

func (b *flakyJoinBackend) WebSocketJoin(ctx context.Context, k ws.ClusterKey, instance ws.InstanceID, id ws.ConnectionID, membership ws.ClusterMembership) (ws.PresenceSnapshot, error) {
	if b.failures.Add(-1) >= 0 {
		if b.conflict {
			return ws.PresenceSnapshot{}, ws.PolicyConflict
		}
		return ws.PresenceSnapshot{}, fault.New(fault.Timeout, "simulated authority latency spike")
	}
	return b.ClusterBackend.WebSocketJoin(ctx, k, instance, id, membership)
}

type gapStream struct {
	pubsub.Stream
	fail <-chan struct{}
}

func (s *gapStream) Next(ctx context.Context) (pubsub.Message, error) {
	read, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-s.fail:
			cancel()
		case <-stop:
		case <-read.Done():
		}
	}()
	return s.Stream.Next(read)
}

type cleanupWaitBackend struct {
	ws.ClusterBackend
	hub    *ws.Hub
	result chan error
}

func (b *cleanupWaitBackend) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	stream, err := b.ClusterBackend.Subscribe(ctx, channels, limits)
	if err != nil {
		return nil, err
	}
	return &cleanupWaitStream{Stream: stream, backend: b}, nil
}

type cleanupWaitStream struct {
	pubsub.Stream
	backend *cleanupWaitBackend
}

func (s *cleanupWaitStream) Close(ctx context.Context) error {
	err := s.backend.hub.Stop(ctx)
	s.backend.result <- err
	return errors.Join(err, s.Stream.Close(ctx))
}
