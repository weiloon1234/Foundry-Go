package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

func TestAcceptedIsBeforeCompletionAndNeverBeforeAuthorization(t *testing.T) {
	channel := publicChannel()
	entered, release := make(chan struct{}), make(chan struct{})
	incoming := ws.DefineIncoming(channel, "save", echoContract()).AcknowledgeAccepted().Authorize(func(_ context.Context, _ ws.MessageContext[int64, ws.Anonymous], p Echo) error {
		if p.Text == "deny" {
			return auth.Forbidden
		}
		return nil
	})
	f := serve(t, registry(t, ws.Register(channel, incoming.Handle(func(_ context.Context, _ ws.MessageContext[int64, ws.Anonymous], p Echo) error {
		if p.Text == "fail" {
			return errors.New("private failure")
		}
		close(entered)
		<-release
		return nil
	}))), nil, ws.DefaultConfig())
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), room("1"))
	message := func(id, text string) {
		send(t, peer, ws.Request{Action: ws.Message, ID: ws.RequestID(id), Channel: channel.ID(), Room: room("1"), Event: "save", Payload: json.RawMessage(`{"text":"` + text + `"}`)})
	}
	message("denied", "deny")
	if receive(t, peer, ws.ErrorResponse).Code != ws.Forbidden {
		t.Fatal("policy bypass")
	}
	message("failed", "fail")
	receive(t, peer, ws.Accepted)
	if receive(t, peer, ws.ErrorResponse).Code != ws.OperationFailed {
		t.Fatal("failure acknowledged as success")
	}
	message("saved", "ok")
	receive(t, peer, ws.Accepted)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not enter")
	}
	close(release)
	if receive(t, peer, ws.Acknowledged).ID != "saved" {
		t.Fatal("completion lost request identity")
	}
}

func TestTypedRelayAndLocalReplayRoomBoundsAndExpiry(t *testing.T) {
	channel := publicChannel().WithReplay(ws.ReplayConfig{Messages: 3, Bytes: 4096, TTL: 150 * time.Millisecond})
	out := ws.DefineOutgoing(channel, "updated", echoContract())
	in := ws.DefineIncoming(channel, "relay", echoContract()).AcknowledgeAccepted()
	f := serve(t, registry(t, ws.Register(channel, out.Registration(), in.Relay(out))), nil, ws.DefaultConfig())
	for _, r := range []int64{1, 2, 1, 2} {
		if _, err := ws.Publish(t.Context(), f.hub, channel, r, out, Echo{"history"}); err != nil {
			t.Fatal(err)
		}
	}
	peer := f.dial(t, nil)
	subscribe(t, peer, "room", channel.ID(), room("1"))
	if r := receive(t, peer, ws.EventResponse); !r.Replayed || r.Room == nil || *r.Room != "1" {
		t.Fatal("replay crossed room or omitted marker")
	}
	// Subscribing to the whole channel does not replay room-specific traffic.
	subscribe(t, peer, "whole", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Message, ID: "relay", Channel: channel.ID(), Room: room("1"), Event: "relay", Payload: json.RawMessage(`{"text":"live"}`)})
	receive(t, peer, ws.Accepted)
	live := receive(t, peer, ws.EventResponse)
	if live.Replayed || live.MessageID.IsZero() {
		t.Fatal("invalid live publication")
	}
	receive(t, peer, ws.Acknowledged)
	// Overlapping whole/room memberships must deliver this broadcast once.
	id, err := ws.Broadcast(t.Context(), f.hub, channel, out, Echo{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if receive(t, peer, ws.EventResponse).MessageID != id {
		t.Fatal("broadcast missing")
	}
	send(t, peer, ws.Request{Action: ws.Unsubscribe, ID: "barrier", Channel: channel.ID()})
	receive(t, peer, ws.Unsubscribed)
	time.Sleep(180 * time.Millisecond)
	other := f.dial(t, nil)
	subscribe(t, other, "expired", channel.ID(), room("1"))
	send(t, other, ws.Request{Action: ws.Unsubscribe, ID: "empty", Channel: channel.ID(), Room: room("1")})
	receive(t, other, ws.Unsubscribed)
}

func TestHeartbeatEvictsSilentPeerAndRateCountsMalformedFrames(t *testing.T) {
	t.Run("silent", func(t *testing.T) {
		config := ws.DefaultConfig()
		config.HeartbeatInterval = 20 * time.Millisecond
		config.PongTimeout = 20 * time.Millisecond
		f := serve(t, registry(t, ws.Register(publicChannel())), nil, config)
		_ = f.dial(t, nil) // Deliberately never read: transport cannot process pings.
		waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
	})
	t.Run("rate", func(t *testing.T) {
		config := ws.DefaultConfig()
		config.MessageRate = ratelimit.Limit{Requests: 2, Window: time.Minute}
		f := serve(t, registry(t, ws.Register(publicChannel())), nil, config)
		peer := f.dial(t, nil)
		for i := 0; i < 3; i++ {
			if err := peer.SendText(t.Context(), []byte(`{`)); err != nil {
				t.Fatal(err)
			}
			r := receive(t, peer, ws.ErrorResponse)
			want := ws.Malformed
			if i == 2 {
				want = ws.RateLimited
			}
			if r.Code != want {
				t.Fatalf("frame %d: %s", i, r.Code)
			}
		}
	})
}

func TestRefreshRevokesIdleSubjectAndLocalSubjectLimitCountsConnections(t *testing.T) {
	a := authentication(t)
	channel := ws.OwnedRooms[AccountChannel]("accounts", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, (Account{}).FoundryReference())
	config := ws.DefaultConfig()
	config.AuthRefreshInterval = 30 * time.Millisecond
	config.MaxConnectionsPerSubject = 1
	f := serve(t, registry(t, ws.Register(channel)), a.transport, config)
	first, second := f.dial(t, userHeaders()), f.dial(t, userHeaders())
	subscribe(t, first, "first", channel.ID(), room("7"))
	send(t, second, ws.Request{Action: ws.Subscribe, ID: "second", Channel: channel.ID(), Room: room("7")})
	if receive(t, second, ws.ErrorResponse).Code != ws.CapacityExceeded {
		t.Fatal("subject limit not enforced")
	}
	second.Close()
	a.disabled.Store(true)
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
}

func TestProtectedDiagnosticsAndTypedDisconnectGuardNamespace(t *testing.T) {
	a := authentication(t)
	users := ws.OwnedRooms[AccountChannel]("accounts", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, (Account{}).FoundryReference())
	admins := ws.OwnedRooms[AdminChannel]("admins", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.admins, (Account{}).FoundryReference())
	f := serve(t, registry(t, ws.Register(users), ws.Register(admins)), a.transport, ws.DefaultConfig())
	first := f.dial(t, userHeaders())
	second := f.dial(t, http.Header{"Cookie": []string{"admin_session=valid-admin"}})
	subscribe(t, first, "user", users.ID(), room("7"))
	subscribe(t, second, "admin", admins.ID(), room("7"))
	allow := func(context.Context, Account) error { return nil }
	if _, err := ws.Diagnose(t.Context(), f.hub, a.users, allow); err == nil {
		t.Fatal("unauthenticated diagnostics allowed")
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/diagnostics", nil)
	request.Header = userHeaders()
	credentials, err := a.transport.CaptureCredentials(request)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := a.transport.Registry().NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if _, err := ws.Diagnose(scope.Context(), f.hub, a.users, func(context.Context, Account) error { return auth.Forbidden }); !errors.Is(err, auth.Forbidden) {
		t.Fatal("management policy bypass")
	}
	if err := ws.DisconnectSubject(t.Context(), f.hub, a.users, Account{ID: 7}.FoundryReference()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 1 })
	send(t, second, ws.Request{Action: ws.Unsubscribe, ID: "alive", Channel: admins.ID(), Room: room("7")})
	receive(t, second, ws.Unsubscribed)
	report, err := ws.Diagnose(scope.Context(), f.hub, a.users, allow)
	if err != nil || report.ForcedDisconnects != 1 || report.Runtime.Connections != 1 {
		t.Fatal("diagnostics missing guarded disconnect", err)
	}
}

func TestHeartbeatChurnReleasesLoopsAndQueues(t *testing.T) {
	baseline := runtime.NumGoroutine()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var left atomic.Int32
	channel := publicChannel().WithHooks(ws.Hooks[int64, ws.Anonymous]{Left: func(context.Context, ws.LeaveContext[int64]) error { left.Add(1); return nil }})
	config := ws.DefaultConfig()
	config.HeartbeatInterval = 20 * time.Millisecond
	config.PongTimeout = 100 * time.Millisecond
	f := serve(t, registry(t, ws.Register(channel)), nil, config)
	for i := 0; i < 32; i++ {
		peer := f.dial(t, nil)
		subscribe(t, peer, "join", channel.ID(), room("1"))
		peer.Close()
	}
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 && left.Load() == 32 })
	if err := f.hub.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return runtime.NumGoroutine() <= baseline+6 })
	snapshot := f.hub.Snapshot()
	if snapshot.Subscriptions != 0 || snapshot.ActiveOperations != 0 || snapshot.BackgroundTasks != 0 {
		t.Fatal("churn retained owned work")
	}
	runtime.ReadMemStats(&after)
	t.Logf("32 connections drained; goroutines baseline=%d current=%d; allocated=%d bytes, %d allocations", baseline, runtime.NumGoroutine(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
}

func TestBlockedAuthorizationRefreshRetainsOwnershipUntilActualExit(t *testing.T) {
	a := authentication(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	channel := ws.Private[AccountChannel]("refresh", ws.DefineRooms(foundryhttp.IntegerPath[int64]()), a.users, func(context.Context, Account, ws.Target[int64]) error {
		if calls.Add(1) > 1 {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
		}
		return nil
	})
	config := ws.DefaultConfig()
	config.AuthRefreshInterval = 20 * time.Millisecond
	config.OperationTimeout = 30 * time.Millisecond
	f := serve(t, registry(t, ws.Register(channel)), a.transport, config)
	defer close(release)
	peer := f.dial(t, userHeaders())
	subscribe(t, peer, "join", channel.ID(), room("1"))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh callback not entered")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := peer.Receive(ctx); err == nil {
		t.Fatal("stale authorization retained socket")
	}
	ctxStop, cancelStop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelStop()
	if err := f.hub.Stop(ctxStop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked callback ownership vanished", err)
	}
	if f.hub.Snapshot().Connections != 1 {
		t.Fatal("callback released connection capacity before exit")
	}
}

func TestStopDrainsAcceptedFramesBeforeGoingAway(t *testing.T) {
	channel := publicChannel()
	out := ws.DefineOutgoing(channel, "updated", echoContract())
	f := serve(t, registry(t, ws.Register(channel, out.Registration())), nil, ws.DefaultConfig())
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), room("1"))
	want := make([]ws.MessageID, 8)
	for i := range want {
		id, err := ws.Publish(t.Context(), f.hub, channel, int64(1), out, Echo{"drain"})
		if err != nil {
			t.Fatal(err)
		}
		want[i] = id
	}
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stopped <- f.hub.Stop(ctx)
	}()
	for _, id := range want {
		if receive(t, peer, ws.EventResponse).MessageID != id {
			t.Fatal("shutdown lost queued frame")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := peer.Receive(ctx); transport.CloseStatus(err) != transport.StatusGoingAway {
		t.Fatal("shutdown omitted going-away close", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestLocalIPCapacityIsReleasedAfterDisconnect(t *testing.T) {
	config := ws.DefaultConfig()
	config.MaxConnectionsPerIP = 1
	f := serve(t, registry(t, ws.Register(publicChannel())), nil, config)
	first := f.dial(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	denied, response, err := transport.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http"), &transport.DialOptions{Subprotocols: []string{ws.Subprotocol}, HTTPHeader: http.Header{"Origin": []string{f.server.URL}}})
	if denied != nil {
		denied.CloseNow()
	}
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("local IP quota bypass")
	}
	first.Close()
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
	next := f.dial(t, nil)
	subscribe(t, next, "released", publicChannel().ID(), nil)
}
