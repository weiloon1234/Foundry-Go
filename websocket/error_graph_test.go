package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

type cyclicSocketError struct{ visits atomic.Int32 }

func (*cyclicSocketError) Error() string { return "private socket failure" }
func (e *cyclicSocketError) Unwrap() error {
	// Let a regression finish so its traversal count fails without leaking a
	// permanently stuck HTTP handler into the rest of the test suite.
	if e.visits.Add(1) > 1024 {
		return nil
	}
	return e
}

type failingRoomCodec struct {
	foundryhttp.PathCodec[int64]
	failure error
}

func (c failingRoomCodec) Parse(text string) (int64, error) {
	if text == "1" {
		return 0, c.failure
	}
	return c.PathCodec.Parse(text)
}

func TestCyclicSocketErrorsReleaseOperationsAndPermitNextRequest(t *testing.T) {
	for _, site := range []string{"handler", "policy", "join", "leave", "room"} {
		t.Run(site, func(t *testing.T) {
			failure := &cyclicSocketError{}
			channel := publicChannel()
			if site == "room" {
				codec := foundryhttp.DescribeURL[int64](failingRoomCodec{foundryhttp.IntegerPath[int64](), failure}, contract.DefineScalar[int64](contract.Type{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true}))
				channel = ws.Public[Chat]("chat", ws.DefineRooms(codec))
			}
			var hookFailed atomic.Bool
			channel = channel.WithHooks(ws.Hooks[int64, ws.Anonymous]{
				Joined: func(context.Context, ws.MessageContext[int64, ws.Anonymous]) error {
					if site == "join" && !hookFailed.Swap(true) {
						return failure
					}
					return nil
				},
				Left: func(context.Context, ws.LeaveContext[int64]) error {
					if site == "leave" && !hookFailed.Swap(true) {
						return failure
					}
					return nil
				},
			})
			in := ws.DefineIncoming(channel, "send", echoContract()).Authorize(func(_ context.Context, _ ws.MessageContext[int64, ws.Anonymous], payload Echo) error {
				if site == "policy" && payload.Text == "fail" {
					return failure
				}
				return nil
			})
			var completed atomic.Int32
			f := serve(t, registry(t, ws.Register(channel, in.Handle(func(_ context.Context, _ ws.MessageContext[int64, ws.Anonymous], payload Echo) error {
				if site == "handler" && payload.Text == "fail" {
					return failure
				}
				completed.Add(1)
				return nil
			}))), nil, ws.DefaultConfig())
			peer := f.dial(t, nil)
			want := ws.OperationFailed
			switch site {
			case "room", "join":
				send(t, peer, ws.Request{Action: ws.Subscribe, ID: "bad", Channel: channel.ID(), Room: room("1")})
				if site == "room" {
					want = ws.Malformed
				}
			case "leave":
				subscribe(t, peer, "initial", channel.ID(), room("2"))
				send(t, peer, ws.Request{Action: ws.Unsubscribe, ID: "bad", Channel: channel.ID(), Room: room("2")})
			default:
				subscribe(t, peer, "initial", channel.ID(), room("2"))
				send(t, peer, ws.Request{Action: ws.Message, ID: "bad", Channel: channel.ID(), Room: room("2"), Event: "send", Payload: json.RawMessage(`{"text":"fail"}`)})
			}
			if response := receive(t, peer, ws.ErrorResponse); response.Code != want || response.ID != "bad" {
				t.Fatal("wrong failure response", response)
			}
			if n := failure.visits.Load(); n < 1 || n > 256 {
				t.Fatal("unbounded error traversal", n)
			}
			if site == "room" || site == "join" || site == "leave" {
				if f.hub.Snapshot().Subscriptions != 0 {
					t.Fatal("failed admission or leave retained a subscription")
				}
				subscribe(t, peer, "retry", channel.ID(), room("2"))
			}
			send(t, peer, ws.Request{Action: ws.Message, ID: "good", Channel: channel.ID(), Room: room("2"), Event: "send", Payload: json.RawMessage(`{"text":"ok"}`)})
			receive(t, peer, ws.Acknowledged)
			if completed.Load() != 1 {
				t.Fatal("failure invoked the handler or prevented recovery")
			}
			peer.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := f.hub.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if state := f.hub.Snapshot(); state.Connections != 0 || state.Subscriptions != 0 || state.ActiveOperations != 0 || state.BackgroundTasks != 0 {
				t.Fatal("failed inspection retained ownership", state)
			}
		})
	}
}

func TestSocketErrorGraphRetainsWireCodePriority(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want ws.Code
	}{
		{"wrapped-auth", fmt.Errorf("hidden: %w", auth.Forbidden), ws.Forbidden},
		{"joined-protocol", errors.Join(auth.Forbidden, ws.Stopping), ws.Stopping},
		{"joined-code-priority", errors.Join(ws.Stopping, ws.InvalidPayload), ws.InvalidPayload},
		{"wrapped-deadline", fmt.Errorf("hidden: %w", context.DeadlineExceeded), ws.OperationTimedOut},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := publicChannel()
			in := ws.DefineIncoming(channel, "send", echoContract())
			f := serve(t, registry(t, ws.Register(channel, in.Handle(func(context.Context, ws.MessageContext[int64, ws.Anonymous], Echo) error { return test.err }))), nil, ws.DefaultConfig())
			peer := f.dial(t, nil)
			subscribe(t, peer, "join", channel.ID(), nil)
			send(t, peer, ws.Request{Action: ws.Message, ID: "failed", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"fail"}`)})
			if response := receive(t, peer, ws.ErrorResponse); response.Code != test.want {
				t.Fatal("classification priority changed", response.Code)
			}
		})
	}
}

type failingClusterCheck struct {
	ws.ClusterBackend
	failure error
}

func (b failingClusterCheck) WebSocketCheck(context.Context, ws.ClusterKey) error { return b.failure }

func TestCyclicClusterErrorFailsStartupAndCompletesShutdown(t *testing.T) {
	failure := &cyclicSocketError{}
	hub, err := ws.NewDistributed(registry(t, ws.Register(publicChannel())), nil, ws.DefaultConfig(), failingClusterCheck{failure: failure}, ws.DefaultClusterConfig(keyspace.Namespace{Application: "socket-errors", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := hub.Start(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("failed cluster setup did not finish")
	}
	if err := hub.Stop(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("terminal cluster failure or cleanup was lost")
	}
	select {
	case <-hub.Done():
	default:
		t.Fatal("cluster retained setup ownership")
	}
	if n := failure.visits.Load(); n < 1 || n > 2*256 {
		t.Fatal("unbounded cluster failure inspection", n)
	}
	if state := hub.Snapshot(); !state.Degraded || !state.Stopping || state.ActiveOperations != 0 || state.BackgroundTasks != 0 || state.Connections != 0 {
		t.Fatal("failed cluster did not drain", state)
	}
}
