package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

type abnormalError struct{ inspect func() }

func (abnormalError) Error() string   { return "private error" }
func (e abnormalError) Is(error) bool { e.inspect(); return false }

func TestCallbackFailuresAreIsolatedAndAcknowledgementsFollowCompletion(t *testing.T) {
	for _, test := range []struct {
		name string
		fail func() error
	}{
		{"error", func() error { return errors.New("private handler details") }},
		{"panic", func() error { panic("private panic") }},
		{"goexit", func() error { runtime.Goexit(); return nil }},
		{"inspection-panic", func() error { return abnormalError{func() { panic("private inspection") }} }},
		{"inspection-goexit", func() error { return abnormalError{runtime.Goexit} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := publicChannel()
			var completed atomic.Int32
			in := ws.DefineIncoming(channel, "send", echoContract())
			f := serve(t, registry(t, ws.Register(channel, in.Handle(func(ctx context.Context, message ws.MessageContext[int64, ws.Anonymous], payload Echo) error {
				if err := message.Publisher.Stop(ctx); !errors.Is(err, fault.Cycle) {
					return errors.New("self-wait was not rejected")
				}
				if payload.Text == "fail" {
					return test.fail()
				}
				completed.Add(1)
				return nil
			}))), nil, ws.DefaultConfig())
			peer := f.dial(t, nil)
			subscribe(t, peer, "join", channel.ID(), nil)
			send(t, peer, ws.Request{Action: ws.Message, ID: "bad", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"fail"}`)})
			if receive(t, peer, ws.ErrorResponse).Code != ws.OperationFailed {
				t.Fatal("failure classification leaked")
			}
			send(t, peer, ws.Request{Action: ws.Message, ID: "good", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"ok"}`)})
			receive(t, peer, ws.Acknowledged)
			if completed.Load() != 1 {
				t.Fatal("acknowledgement preceded handler completion")
			}
		})
	}
}

func TestCancelledCallbackKeepsConnectionSlotUntilActualExit(t *testing.T) {
	channel := publicChannel()
	cancelled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	in := ws.DefineIncoming(channel, "send", echoContract())
	config := ws.DefaultConfig()
	config.OperationTimeout = 30 * time.Millisecond
	config.MaxConnections = 1
	f := serve(t, registry(t, ws.Register(channel, in.Handle(func(ctx context.Context, _ ws.MessageContext[int64, ws.Anonymous], _ Echo) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}))), nil, config)
	peer := f.dial(t, nil)
	subscribe(t, peer, "join", channel.ID(), nil)
	send(t, peer, ws.Request{Action: ws.Message, ID: "blocked", Channel: channel.ID(), Event: "send", Payload: json.RawMessage(`{"text":"x"}`)})
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("operation timeout not observed")
	}
	stop, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := f.hub.Stop(stop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown abandoned callback")
	}
	if f.hub.Snapshot().Connections != 1 {
		t.Fatal("live callback released capacity")
	}
	select {
	case <-f.hub.Done():
		t.Fatal("done before actual exit")
	default:
	}
	release <- struct{}{}
	ctx, done := context.WithTimeout(t.Context(), 3*time.Second)
	defer done()
	if err := f.hub.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if f.hub.Snapshot().Connections != 0 {
		t.Fatal("connection slot retained after callback exit")
	}
}

func TestSuccessfulJoinGetsOneLeaveOnUnsubscribeOrDisconnect(t *testing.T) {
	var joined, left atomic.Int32
	channel := publicChannel().WithHooks(ws.Hooks[int64, ws.Anonymous]{Joined: func(context.Context, ws.MessageContext[int64, ws.Anonymous]) error { joined.Add(1); return nil }, Left: func(context.Context, ws.LeaveContext[int64]) error { left.Add(1); return nil }})
	f := serve(t, registry(t, ws.Register(channel)), nil, ws.DefaultConfig())
	peer := f.dial(t, nil)
	subscribe(t, peer, "one", channel.ID(), room("1"))
	subscribe(t, peer, "two", channel.ID(), room("2"))
	send(t, peer, ws.Request{Action: ws.Subscribe, ID: "duplicate", Channel: channel.ID(), Room: room("1")})
	receive(t, peer, ws.ErrorResponse)
	send(t, peer, ws.Request{Action: ws.Unsubscribe, ID: "leave", Channel: channel.ID(), Room: room("1")})
	receive(t, peer, ws.Unsubscribed)
	peer.Close()
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
	if joined.Load() != 2 || left.Load() != 2 {
		t.Fatal("lifecycle callbacks duplicated or lost")
	}
}

func TestCancelledJoinBalancesCleanupBeforeConnectionRelease(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var left atomic.Int32
	channel := publicChannel().WithHooks(ws.Hooks[int64, ws.Anonymous]{Joined: func(ctx context.Context, _ ws.MessageContext[int64, ws.Anonymous]) error {
		close(started)
		<-ctx.Done()
		<-release
		return nil
	}, Left: func(context.Context, ws.LeaveContext[int64]) error { left.Add(1); return nil }})
	f := serve(t, registry(t, ws.Register(channel)), nil, ws.DefaultConfig())
	peer := f.dial(t, nil)
	send(t, peer, ws.Request{Action: ws.Subscribe, ID: "join", Channel: channel.ID()})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("join never started")
	}
	peer.Close()
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 1 })
	release <- struct{}{}
	waitFor(t, func() bool { return f.hub.Snapshot().Connections == 0 })
	if left.Load() != 1 || f.hub.Snapshot().Subscriptions != 0 {
		t.Fatal("late joined hook leaked ownership")
	}
}
