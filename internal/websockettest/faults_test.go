package websockettest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

// fakeCluster is an in-memory authority over the memory pub/sub adapter with
// fault injection, for cluster failure paths that need no Redis. It keeps the
// Redis script's membership semantics that these tests rely on: a presence
// change advances the revision and a differing re-join is a membership
// conflict. Quotas, lease expiry and replay are not modelled.
type fakeCluster struct {
	*memory.Backend
	mu        sync.Mutex
	revision  uint64
	owners    map[ws.ConnectionID]ws.InstanceID
	members   map[ws.ConnectionID]map[ws.Scope]ws.ClusterMembership
	failures  map[string][]error
	blocked   map[string]int
	calls     map[string][]time.Time
	published [][]byte
	topic     pubsub.Channel
	lose      chan struct{}
	exit      chan struct{}
}

func newFakeCluster(t *testing.T) *fakeCluster {
	t.Helper()
	backend, err := memory.New(64)
	must(t, err)
	t.Cleanup(func() { _ = backend.Close() })
	return &fakeCluster{Backend: backend, owners: make(map[ws.ConnectionID]ws.InstanceID), members: make(map[ws.ConnectionID]map[ws.Scope]ws.ClusterMembership),
		failures: make(map[string][]error), blocked: make(map[string]int), calls: make(map[string][]time.Time), lose: make(chan struct{}, 1), exit: make(chan struct{}, 1)}
}

// fail queues errors returned by the next calls of op.
func (f *fakeCluster) fail(op string, errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], errs...)
}

// block makes the next n calls of op wait for their context to end.
func (f *fakeCluster) block(op string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked[op] += n
}
func (f *fakeCluster) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls[op])
}
func (f *fakeCluster) times(op string) []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls[op])
}
func (f *fakeCluster) enter(ctx context.Context, op string) error {
	f.mu.Lock()
	f.calls[op] = append(f.calls[op], time.Now())
	if f.blocked[op] > 0 {
		f.blocked[op]--
		f.mu.Unlock()
		<-ctx.Done()
		return ctx.Err()
	}
	if queued := f.failures[op]; len(queued) > 0 {
		f.failures[op] = queued[1:]
		f.mu.Unlock()
		return queued[0]
	}
	f.mu.Unlock()
	return ctx.Err()
}

// snapshotLocked aggregates the scope's presence members by subject.
func (f *fakeCluster) snapshotLocked(scope ws.Scope) ws.PresenceSnapshot {
	if f.revision == 0 {
		f.revision = 1
	}
	bySubject := make(map[ws.MemberID]*ws.MemberFrame)
	for _, scopes := range f.members {
		if m, ok := scopes[scope]; ok && m.Presence {
			if frame := bySubject[m.Subject]; frame != nil {
				frame.Connections++
			} else {
				bySubject[m.Subject] = &ws.MemberFrame{ID: m.Subject, Data: bytes.Clone(m.Data), Connections: 1}
			}
		}
	}
	snapshot := ws.PresenceSnapshot{Revision: f.revision, Members: []ws.MemberFrame{}}
	for _, frame := range bySubject {
		snapshot.Members = append(snapshot.Members, *frame)
	}
	slices.SortFunc(snapshot.Members, func(a, b ws.MemberFrame) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return snapshot
}

func (f *fakeCluster) WebSocketCheck(ctx context.Context, _ ws.ClusterKey) error {
	return f.enter(ctx, "check")
}
func (f *fakeCluster) WebSocketOpen(ctx context.Context, _ ws.ClusterKey, instance ws.InstanceID, id ws.ConnectionID) error {
	if err := f.enter(ctx, "open"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[id] = instance
	return nil
}
func (f *fakeCluster) WebSocketTouch(ctx context.Context, _ ws.ClusterKey, instance ws.InstanceID, id ws.ConnectionID) error {
	if err := f.enter(ctx, "touch"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owners[id] != instance {
		return ws.Stopping
	}
	return nil
}
func (f *fakeCluster) WebSocketJoin(ctx context.Context, _ ws.ClusterKey, _ ws.InstanceID, id ws.ConnectionID, m ws.ClusterMembership) (ws.PresenceSnapshot, error) {
	if err := f.enter(ctx, "join"); err != nil {
		return ws.PresenceSnapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if prior, ok := f.members[id][m.Scope]; ok && (prior.Subject != m.Subject || prior.Presence != m.Presence || !bytes.Equal(prior.Data, m.Data)) {
		return ws.PresenceSnapshot{}, ws.MembershipConflict
	}
	if f.members[id] == nil {
		f.members[id] = make(map[ws.Scope]ws.ClusterMembership)
	}
	m.Data = bytes.Clone(m.Data)
	f.members[id][m.Scope] = m
	if m.Presence {
		f.revision++
	}
	return f.snapshotLocked(m.Scope), nil
}
func (f *fakeCluster) WebSocketLeave(ctx context.Context, _ ws.ClusterKey, _ ws.InstanceID, id ws.ConnectionID, scope ws.Scope) error {
	if err := f.enter(ctx, "leave"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if prior, ok := f.members[id][scope]; ok {
		if prior.Presence {
			f.revision++
		}
		delete(f.members[id], scope)
	}
	return nil
}
func (f *fakeCluster) WebSocketClose(ctx context.Context, _ ws.ClusterKey, _ ws.InstanceID, id ws.ConnectionID) error {
	if err := f.enter(ctx, "close"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members[id] {
		if m.Presence {
			f.revision++
			break
		}
	}
	delete(f.members, id)
	delete(f.owners, id)
	return nil
}
func (f *fakeCluster) WebSocketMembers(ctx context.Context, _ ws.ClusterKey, scope ws.Scope) (ws.PresenceSnapshot, error) {
	if err := f.enter(ctx, "members"); err != nil {
		return ws.PresenceSnapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshotLocked(scope), nil
}
func (f *fakeCluster) WebSocketAppend(ctx context.Context, _ ws.ClusterKey, _ ws.ChannelID, _ ws.ReplayConfig, _ []byte) error {
	return f.enter(ctx, "append")
}
func (f *fakeCluster) WebSocketHistory(ctx context.Context, _ ws.ClusterKey, _ ws.ChannelID, _ ws.ReplayConfig) ([][]byte, error) {
	return nil, f.enter(ctx, "history")
}

// Publish records every fan-out envelope for wire assertions.
func (f *fakeCluster) Publish(ctx context.Context, channel pubsub.Channel, data []byte) (uint64, error) {
	f.mu.Lock()
	f.topic = channel
	f.published = append(f.published, bytes.Clone(data))
	f.mu.Unlock()
	return f.Backend.Publish(ctx, channel, data)
}

// inject delivers a raw envelope to subscribers without recording it.
func (f *fakeCluster) inject(t *testing.T, data []byte) {
	t.Helper()
	f.mu.Lock()
	topic := f.topic
	f.mu.Unlock()
	_, err := f.Backend.Publish(t.Context(), topic, data)
	must(t, err)
}

// envelopes returns recorded envelopes of one kind, decoded as JSON objects.
func (f *fakeCluster) envelopes(t *testing.T, kind string) []map[string]json.RawMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []map[string]json.RawMessage
	for _, data := range f.published {
		var envelope map[string]json.RawMessage
		must(t, json.Unmarshal(data, &envelope))
		if string(envelope["kind"]) == `"`+kind+`"` {
			result = append(result, envelope)
		}
	}
	return result
}

func (f *fakeCluster) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	if err := f.enter(ctx, "subscribe"); err != nil {
		return nil, err
	}
	stream, err := f.Backend.Subscribe(ctx, channels, limits)
	if err != nil {
		return nil, err
	}
	return &faultStream{Stream: stream, lose: f.lose, exit: f.exit}, nil
}

// faultStream loses its delivery or calls runtime.Goexit in the reading
// goroutine (the hub's receive loop) when signalled.
type faultStream struct {
	pubsub.Stream
	lose <-chan struct{}
	exit <-chan struct{}
}

func (s *faultStream) Next(ctx context.Context) (pubsub.Message, error) {
	type result struct {
		message pubsub.Message
		err     error
	}
	read, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, 1)
	go func() {
		message, err := s.Stream.Next(read)
		results <- result{message, err}
	}()
	select {
	case r := <-results:
		return r.message, r.err
	case <-s.lose:
		cancel()
		<-results
		return pubsub.Message{}, pubsub.ErrDisconnected
	case <-s.exit:
		cancel()
		<-results
		runtime.Goexit()
		return pubsub.Message{}, nil
	}
}

func faultConfig(suffix string) ws.ClusterConfig {
	return ws.DefaultClusterConfig(keyspace.Namespace{Application: "faults", Environment: "test-" + suffix})
}

func TestTransientFailuresFailOnlyTheOperationAndPolicyConflictStops(t *testing.T) {
	channel := ws.Public[RoomOwner]("transient", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
	f := newFakeCluster(t)
	s := serve(t, registry(t, ws.Register(channel)), nil, f, faultConfig("transient"))
	p := s.dial(t, false)
	f.fail("join", fault.New(fault.Timeout, "simulated latency spike"))
	p.send(t, ws.Request{Action: ws.Subscribe, ID: "blip", Channel: channel.ID(), Room: room("1")})
	if code := p.next(t, ws.ErrorResponse).Code; code != ws.Unavailable {
		t.Fatal("transient failure was not retryable", code)
	}
	if snapshot := s.hub.Snapshot(); snapshot.Stopping || snapshot.Failures == 0 {
		t.Fatal("transient failure stopped the hub or was not counted", snapshot)
	}
	p.subscribe(t, "retry", channel.ID(), room("1"), nil)
	if snapshot := s.hub.Snapshot(); snapshot.Degraded || snapshot.Stopping {
		t.Fatal("successful retry did not recover", snapshot)
	}
	// A record left by a failed leave conflicts with a different re-join: the
	// hub releases it and joins again instead of treating it as policy.
	f.fail("leave", fault.New(fault.Timeout, "simulated failover"))
	p.send(t, ws.Request{Action: ws.Unsubscribe, ID: "leave", Channel: channel.ID(), Room: room("1")})
	// The local subscription is released; the failed remote leave is reported.
	if code := p.next(t, ws.ErrorResponse).Code; code != ws.Unavailable {
		t.Fatal("failed leave was not reported as retryable", code)
	}
	f.mu.Lock()
	for id := range f.members {
		for scope, m := range f.members[id] {
			m.Data = json.RawMessage(`{"stale":true}`)
			f.members[id][scope] = m
		}
	}
	f.mu.Unlock()
	leaves := f.count("leave")
	p.subscribe(t, "rejoin", channel.ID(), room("1"), nil)
	if f.count("leave") != leaves+1 || s.hub.Snapshot().Stopping {
		t.Fatal("membership conflict was not replaced by leave and join")
	}
	f.fail("join", ws.PolicyConflict)
	other := s.dial(t, false)
	other.send(t, ws.Request{Action: ws.Subscribe, ID: "conflict", Channel: channel.ID(), Room: room("2")})
	eventually(t, func() bool { return s.hub.Snapshot().Stopping })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := s.hub.Stop(ctx); !errors.Is(err, ws.PolicyConflict) {
		t.Fatal("policy conflict was not terminal", err)
	}
}

func TestStreamLossClosesWith1013AndResubscribesWithBackoff(t *testing.T) {
	channel := ws.Public[RoomOwner]("gaps", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
	out := ws.DefineOutgoing(channel, "updated", textContract[Payload]())
	f := newFakeCluster(t)
	s := serve(t, registry(t, ws.Register(channel, out.Registration())), nil, f, faultConfig("gaps"))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	socket, err := client.Dial(ctx, "ws"+strings.TrimPrefix(s.http.URL, "http"), http.Header{"Origin": []string{s.http.URL}}, 64<<10)
	must(t, err)
	defer socket.Close()
	must(t, socket.Send(ctx, ws.Request{Version: ws.ProtocolVersion, Action: ws.Subscribe, ID: "join", Channel: channel.ID(), Room: room("1")}))
	if reply, err := socket.Receive(ctx); err != nil || reply.Type != ws.Subscribed {
		t.Fatal(reply, err)
	}
	// The first two resubscription checks fail, so the delays must grow.
	f.fail("check", fault.New(fault.Timeout, "authority still recovering"), fault.New(fault.Timeout, "authority still recovering"))
	checks := f.count("check")
	lost := time.Now()
	f.lose <- struct{}{}
	_, err = socket.Receive(ctx)
	if status := transport.CloseStatus(err); status != transport.StatusTryAgainLater {
		t.Fatal("gapped socket was not closed with 1013", status, err)
	}
	eventually(t, func() bool { s := s.hub.Snapshot(); return s.Streaming && s.Resubscriptions == 1 })
	attempts := f.times("check")[checks:]
	if len(attempts) != 3 {
		t.Fatal("unexpected resubscription attempts", len(attempts))
	}
	first, second, third := attempts[0].Sub(lost), attempts[1].Sub(attempts[0]), attempts[2].Sub(attempts[1])
	if first < 45*time.Millisecond || second < 95*time.Millisecond || third < 190*time.Millisecond {
		t.Fatal("resubscription did not back off exponentially", first, second, third)
	}
	if snapshot := s.hub.Snapshot(); snapshot.Gaps != 1 || snapshot.Degraded || snapshot.Stopping {
		t.Fatal("recovered gap left the hub degraded", snapshot)
	}
	again := s.dial(t, false)
	again.subscribe(t, "again", channel.ID(), room("1"), nil)
	id, err := ws.Publish(t.Context(), s.hub, channel, int64(1), out, Payload{"after"})
	must(t, err)
	if again.next(t, ws.EventResponse).MessageID != id {
		t.Fatal("resubscribed stream did not deliver")
	}
}

func TestRetainedConnectionsSurviveGapsAndPresenceIsResynchronized(t *testing.T) {
	adapter, guard := authenticated(t)
	channel := ws.Private[PresenceOwner]("members", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), guard, func(context.Context, Account, ws.Target[int64]) error { return nil })
	presence := ws.WithPresence(channel, textContract[Member](), func(context.Context, Account) (Member, error) { return Member{"local"}, nil })
	f := newFakeCluster(t)
	cluster := faultConfig("retain")
	cluster.RetainConnectionsOnGap = true
	s := serve(t, registry(t, ws.Register(presence.Channel())), adapter, f, cluster)
	p := s.dial(t, true)
	p.subscribe(t, "join", channel.ID(), room("1"), nil)
	// Let the refresh triggered by this join finish first.
	eventually(t, func() bool { return f.count("members") >= 1 })
	time.Sleep(100 * time.Millisecond)
	// A remote member joined while this instance was not listening: only the
	// resynchronization after the gap can reveal it.
	remote, err := model.NewID[ws.Connection]()
	must(t, err)
	scope := ws.Scope{Channel: channel.ID(), Room: "1", HasRoom: true}
	f.mu.Lock()
	f.members[remote] = map[ws.Scope]ws.ClusterMembership{scope: {Scope: scope, Subject: ws.MemberID(strings.Repeat("b", 64)), Presence: true, Data: json.RawMessage(`{"text":"remote"}`)}}
	f.revision++
	f.mu.Unlock()
	f.lose <- struct{}{}
	for {
		// Skip this connection's own join; the remote one follows the gap.
		joined := p.next(t, ws.PresenceJoined)
		if joined.Member != nil && joined.Member.ID == ws.MemberID(strings.Repeat("b", 64)) {
			break
		}
	}
	if snapshot := s.hub.Snapshot(); snapshot.Connections != 1 || snapshot.Resubscriptions != 1 || snapshot.Gaps != 1 {
		t.Fatal("retained connection was closed by the gap or presence refreshed before resubscription", snapshot)
	}
	// A burst of presence changes is coalesced into few authority reads.
	changes := f.envelopes(t, "presence_changed")
	if len(changes) == 0 {
		t.Fatal("join published no presence change")
	}
	burst, err := json.Marshal(changes[0])
	must(t, err)
	before := f.count("members")
	for range 40 {
		f.inject(t, burst)
	}
	eventually(t, func() bool { return f.count("members") > before })
	time.Sleep(200 * time.Millisecond)
	if reads := f.count("members") - before; reads > 3 {
		t.Fatal("presence refresh was not debounced", reads)
	}
}

func TestLeaseRenewalToleratesTransientFailuresUntilTheLeaseEnds(t *testing.T) {
	channel := ws.Public[RoomOwner]("leases", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
	f := newFakeCluster(t)
	cluster := faultConfig("leases")
	cluster.ConnectionTTL = 600 * time.Millisecond
	cluster.OperationTimeout = 100 * time.Millisecond
	s := serve(t, registry(t, ws.Register(channel)), nil, f, cluster)
	p := s.dial(t, false)
	p.subscribe(t, "join", channel.ID(), room("1"), nil)
	// One renewal times out: the lease still has room for another attempt.
	f.block("touch", 1)
	touches := f.count("touch")
	eventually(t, func() bool { return f.count("touch") >= touches+2 })
	p.send(t, ws.Request{Action: ws.Unsubscribe, ID: "alive", Channel: channel.ID(), Room: room("1")})
	p.next(t, ws.Unsubscribed)
	if s.hub.Snapshot().Connections != 1 {
		t.Fatal("a tolerated renewal failure closed the connection")
	}
	// Renewals that keep failing end the connection before its lease expires.
	f.fail("touch", slices.Repeat([]error{fault.New(fault.Timeout, "authority unavailable")}, 64)...)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("connection outlived its unrenewed lease")
	}
	if s.hub.Snapshot().Stopping {
		t.Fatal("lease loss stopped the hub")
	}
}

func TestAdapterGoexitInReceiveResubscribes(t *testing.T) {
	channel := ws.Public[RoomOwner]("goexit", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
	f := newFakeCluster(t)
	s := serve(t, registry(t, ws.Register(channel)), nil, f, faultConfig("goexit"))
	subscriptions := f.count("subscribe")
	f.exit <- struct{}{}
	eventually(t, func() bool {
		snapshot := s.hub.Snapshot()
		return snapshot.Streaming && snapshot.Resubscriptions == 1 && f.count("subscribe") == subscriptions+1
	})
	if snapshot := s.hub.Snapshot(); snapshot.Stopping || snapshot.Gaps != 1 {
		t.Fatal("adapter Goexit stopped the hub or was not recorded as a gap", snapshot)
	}
}

func TestExclusionsNeverReachOlderInstancesAndUnknownEnvelopesAreDropped(t *testing.T) {
	channel := ws.Public[RoomOwner]("relays", ws.DefineRooms(foundryhttp.IntegerPath[int64]()))
	out := ws.DefineOutgoing(channel, "updated", textContract[Payload]())
	incoming := ws.DefineIncoming(channel, "relay", textContract[Payload]())
	r := registry(t, ws.Register(channel, out.Registration(), incoming.RelayToOthers(out)))
	f := newFakeCluster(t)
	s := serve(t, r, nil, f, faultConfig("relays"))
	sender, receiver := s.dial(t, false), s.dial(t, false)
	sender.subscribe(t, "a", channel.ID(), room("1"), nil)
	receiver.subscribe(t, "b", channel.ID(), room("1"), nil)
	sender.send(t, ws.Request{Action: ws.Message, ID: "relay", Channel: channel.ID(), Room: room("1"), Event: "relay", Payload: payload("hello")})
	relayed := receiver.next(t, ws.EventResponse)
	sender.next(t, ws.Acknowledged)
	// The next publication is ordered after the relay's echo on the stream.
	barrier, err := ws.Publish(t.Context(), s.hub, channel, int64(1), out, Payload{"barrier"})
	must(t, err)
	if got := sender.next(t, ws.EventResponse).MessageID; got != barrier {
		t.Fatal("the sender received its own relay")
	}
	if receiver.next(t, ws.EventResponse).MessageID != barrier || relayed.MessageID == barrier {
		t.Fatal("receiver missed the ordered publications")
	}
	for _, envelope := range f.envelopes(t, "publication") {
		if _, carried := envelope["connection"]; carried {
			t.Fatal("a local exclusion was sent in the fan-out envelope")
		}
	}
	// A connection of another instance can be excluded only once the fleet
	// is known to understand the envelope field.
	elsewhere, err := model.NewID[ws.Connection]()
	must(t, err)
	if _, err := ws.Publish(t.Context(), s.hub, channel, int64(1), out, Payload{"x"}, ws.ExceptConnection(elsewhere)); !ws.NotPublished(err) || !errors.Is(err, fault.Invalid) {
		t.Fatal("remote exclusion was published without the fleet setting", err)
	}
	// An envelope with a member this release does not know is dropped alone.
	publications := f.envelopes(t, "publication")
	extended := publications[len(publications)-1]
	extended["future"] = json.RawMessage(`1`)
	data, err := json.Marshal(extended)
	must(t, err)
	f.inject(t, data)
	eventually(t, func() bool { return s.hub.Snapshot().DroppedEnvelopes == 1 })
	if snapshot := s.hub.Snapshot(); snapshot.Stopping || !snapshot.Streaming {
		t.Fatal("an unknown envelope member stopped the stream", snapshot)
	}
	next, err := ws.Publish(t.Context(), s.hub, channel, int64(1), out, Payload{"after"})
	must(t, err)
	if receiver.next(t, ws.EventResponse).MessageID != next {
		t.Fatal("delivery did not continue after a dropped envelope")
	}

	upgraded := faultConfig("upgraded")
	upgraded.ExcludeRemoteConnections = true
	g := newFakeCluster(t)
	fleet := serve(t, r, nil, g, upgraded)
	if _, err := ws.Publish(t.Context(), fleet.hub, channel, int64(1), out, Payload{"x"}, ws.ExceptConnection(elsewhere)); err != nil {
		t.Fatal(err)
	}
	if envelopes := g.envelopes(t, "publication"); len(envelopes) != 1 || string(envelopes[0]["connection"]) != `"`+elsewhere.String()+`"` {
		t.Fatal("an upgraded fleet did not carry the remote exclusion")
	}
}

var _ ws.ClusterBackend = (*fakeCluster)(nil)
