package notifications

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotificationRevocationDuringRenderingPreventsAdmission(t *testing.T) {
	a := newAuthority(t)
	var sends atomic.Int32
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		a.set(Member{ID: 1, Enabled: false, Allowed: true})
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) { sends.Add(1); return Accepted, nil }))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	if r, err := p.Send(t.Context(), m); err != nil || r.Channels[0].State != Ineligible || sends.Load() != 0 {
		t.Fatal("revocation during render bypassed", r, err)
	}
}

func TestConcurrentFailedRendererCannotRejectAnotherPreparedSnapshot(t *testing.T) {
	a := newAuthority(t)
	var renders, sends atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		if renders.Add(1) == 1 {
			close(entered)
			<-release
			return InboxData{}, errors.New("first renderer failed")
		}
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) { sends.Add(1); return Retry, nil }))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	done := make(chan Report[Member], 1)
	go func() { r, _ := p.Send(t.Context(), m); done <- r }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("renderer did not start")
	}
	if r, err := p.Send(t.Context(), m); err == nil || r.Channels[0].State != Prepared {
		t.Fatal("second renderer did not prepare", r, err)
	}
	unblock()
	select {
	case r := <-done:
		if r.Channels[0].State != Prepared || sends.Load() != 1 {
			t.Fatal("failed renderer overwrote prepared snapshot")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renderer did not finish")
	}
}
