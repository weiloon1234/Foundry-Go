package notifications

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type failingClock struct{ fail atomic.Bool }

func (c *failingClock) Now() time.Time {
	if c.fail.Load() {
		return time.Time{}
	}
	return time.Now()
}

func TestNotificationLostOutcomePersistenceNeverResendsClaim(t *testing.T) {
	a := newAuthority(t)
	clock := new(failingClock)
	var calls atomic.Int32
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) {
		calls.Add(1)
		clock.fail.Store(true)
		return Accepted, nil
	}))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
	m, _ := fixture(t, b.Registration())
	m.config.Clock = clock
	p := captureNotification(t, b, 1, "original")
	if r, err := p.Send(t.Context(), m); err == nil || r.Channels[0].State != Running {
		t.Fatal("failed outcome write was not inspectable", r, err)
	}
	clock.fail.Store(false)
	if r, err := p.Send(t.Context(), m); err == nil || r.Channels[0].State != Running || calls.Load() != 1 {
		t.Fatal("abandoned claim automatically resent", r, err)
	}
}
