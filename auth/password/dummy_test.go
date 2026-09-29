package password

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestDummyCheckUsesSharedHashCapacity(t *testing.T) {
	config := DefaultConfig()
	config.MaxConcurrent = 1
	config.Timeout = 50 * time.Millisecond // Also bounds the queued admission wait.
	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := NewPlaintext(secret.New("private input"))
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.gate.Execute(t.Context(), func(_ context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	err = h.DummyCheck(t.Context(), plain)
	close(release)
	if gateErr := <-done; gateErr != nil {
		t.Fatal(gateErr)
	}
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("dummy work bypassed hasher admission", err)
	}
	if err := h.DummyCheck(t.Context(), plain); err != nil {
		t.Fatal(err)
	}
}
