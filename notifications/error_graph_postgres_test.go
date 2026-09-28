package notifications

import (
	"context"
	"sync/atomic"
	"testing"
)

type cyclicRecipientError struct{ visits atomic.Int32 }

func (*cyclicRecipientError) Error() string { panic("private recipient error formatted") }
func (e *cyclicRecipientError) Unwrap() error {
	// Fail by assertion, rather than hanging, with the old unbounded search.
	if e.visits.Add(1) > 512 {
		return nil
	}
	return e
}

func TestNotificationCyclicRecipientLookupRemainsRetryableAndReleasesOwnership(t *testing.T) {
	a := newAuthority(t)
	cycle := &cyclicRecipientError{}
	a.mu.Lock()
	a.lookupFailure = cycle
	a.mu.Unlock()
	var renders, sends atomic.Int32
	channel := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, input Input) (InboxData, error) {
		renders.Add(1)
		return InboxData{input.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) {
		sends.Add(1)
		return Accepted, nil
	}))
	binding := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, channel)
	manager, _ := fixture(t, binding.Registration())
	pending := captureNotification(t, binding, 1, "original")
	report, err := pending.Send(t.Context(), manager)
	if err == nil || !report.Retryable() || len(report.Channels) != 1 || report.Channels[0].State != Pending || report.Channels[0].Attempts != 0 {
		t.Fatal("failed lookup changed pending delivery")
	}
	if renders.Load() != 0 || sends.Load() != 0 {
		t.Fatal("failed lookup reached render or transport")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("recipient error inspection was not bounded", cycle.visits.Load())
	}
	manager.mu.Lock()
	active := manager.active
	manager.mu.Unlock()
	if active != 0 {
		t.Fatal("returned lookup retained manager ownership")
	}
	a.mu.Lock()
	a.lookupFailure = nil
	a.mu.Unlock()
	report, err = pending.Send(t.Context(), manager)
	if err != nil || report.Retryable() || len(report.Channels) != 1 || report.Channels[0].State != Delivered || report.Channels[0].Attempts != 1 || renders.Load() != 1 || sends.Load() != 1 {
		t.Fatal("recovered lookup did not deliver once")
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-manager.Done():
	default:
		t.Fatal("manager retained shutdown ownership")
	}
}
