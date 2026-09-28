package websocket_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ws "github.com/weiloon1234/Foundry-Go/websocket"
)

// This explicit contract emits a text DTO while its codec deliberately ignores
// cancellation, exercising ownership rather than the cooperative fast path.
type BlockedPublication struct {
	Started chan<- struct{} `json:"-"`
	Release <-chan struct{} `json:"-"`
}

func (p BlockedPublication) MarshalJSON() ([]byte, error) {
	close(p.Started)
	<-p.Release
	return []byte(`{"text":"released"}`), nil
}
func TestPublicationCodecRetainsOwnershipUntilActualExit(t *testing.T) {
	channel := publicChannel()
	outgoing := ws.DefineOutgoing(channel, "blocked", textContract[BlockedPublication]("text"))
	f := serve(t, registry(t, ws.Register(channel, outgoing.Registration())), nil, ws.DefaultConfig())
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := ws.Broadcast(t.Context(), f.hub, channel, outgoing, BlockedPublication{started, release})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("codec did not start")
	}
	stop, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := f.hub.Stop(stop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stop abandoned active codec")
	}
	if f.hub.Snapshot().ActiveOperations != 1 {
		t.Fatal("codec lost its owned capacity")
	}
	select {
	case <-f.hub.Done():
		t.Fatal("hub closed before codec return")
	default:
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled codec published")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publication did not finish")
	}
	finish, cleanup := context.WithTimeout(t.Context(), 3*time.Second)
	defer cleanup()
	if err := f.hub.Stop(finish); err != nil {
		t.Fatal(err)
	}
	snapshot := f.hub.Snapshot()
	if snapshot.ActiveOperations != 0 || snapshot.Publications != 0 {
		t.Fatal("cancelled publication retained capacity or dispatched")
	}
}
