package notifications

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/model"
)

// managerFor builds another manager over an existing notification schema, as a
// newer deployment with a changed binding would.
func managerFor(t *testing.T, w outboxtest.Writer, registrations ...Registration) *Manager {
	t.Helper()
	r, err := NewRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.Schema = w.Schema
	m, err := New(w.DB, r, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}

func customChannel(id ChannelID, outcome func() Outcome) Channel[Member, Input] {
	return Custom(id, textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) { return outcome(), nil }))
}

// Adding or retiring a channel in a later deployment must not make older
// notifications unreadable: they keep the channels they were captured with.
func TestNotificationChannelSetChangeKeepsStoredNotificationsDeliverable(t *testing.T) {
	a := newAuthority(t)
	d := Define("orders.shipped", 1, textSchema[Input]())
	var sent, extra atomic.Int32
	retryOnce := func() Outcome {
		if sent.Add(1) == 1 {
			return Retry
		}
		return Accepted
	}
	original := Bind(d, a.recipient, databaseChannel().Channel(), customChannel("custom", retryOnce))
	m, w := fixture(t, original.Registration())
	p := captureNotification(t, original, 1, "shipped")
	if report, err := p.Send(t.Context(), m); err == nil || !report.Retryable() {
		t.Fatal("expected a retryable first attempt", err)
	}
	added := Bind(d, a.recipient, databaseChannel().Channel(), customChannel("custom", retryOnce),
		customChannel("extra", func() Outcome { extra.Add(1); return Accepted }))
	next := managerFor(t, w, added.Registration())
	statuses, err := next.Deliver(t.Context(), model.IDFromBytes[Notification](p.ID().Bytes()))
	if err != nil {
		t.Fatal("added channel bricked an older notification", err)
	}
	if len(statuses) != 2 || sent.Load() != 2 || extra.Load() != 0 {
		t.Fatalf("older notification changed its captured channels: %+v extra=%d", statuses, extra.Load())
	}
	// Recapturing the same notification ID with the same input stays idempotent.
	again, err := added.Capture(t.Context(), Member{ID: 1}.FoundryReference(), Input{"shipped"}, p.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Send(t.Context(), next); err != nil {
		t.Fatal("identical recapture conflicted after a channel change", err)
	}
	retired := Bind(d, a.recipient, databaseChannel().Channel())
	if _, err := managerFor(t, w, retired.Registration()).Deliver(t.Context(), model.IDFromBytes[Notification](p.ID().Bytes())); err != nil {
		t.Fatal("retired channel bricked an older notification", err)
	}
}

func TestNotificationOperatorListsAndResolvesUncertainDeliveries(t *testing.T) {
	a := newAuthority(t)
	var calls atomic.Int32
	outcome := func() Outcome {
		if calls.Add(1) == 1 {
			return Unknown
		}
		return Accepted
	}
	b := Bind(Define("invoice.sent", 1, textSchema[Input]()), a.recipient, customChannel("custom", outcome))
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "invoice")
	if report, err := p.Send(t.Context(), m); err == nil || report.Channels[0].State != Uncertain {
		t.Fatal("expected an uncertain delivery", err)
	}
	listed, err := m.Deliveries(t.Context(), Uncertain, 10, DeliveryID{})
	if err != nil || len(listed) != 1 || listed[0].Channel != "custom" || listed[0].Attempts != 1 {
		t.Fatalf("uncertain delivery not listed: %+v %v", listed, err)
	}
	if changed, err := m.ResolveDelivery(t.Context(), listed[0].ID, Running, ResolveResend); err != nil || changed {
		t.Fatal("resolution ignored the observed state", changed, err)
	}
	if changed, err := m.ResolveDelivery(t.Context(), listed[0].ID, Uncertain, ResolveResend); err != nil || !changed {
		t.Fatal("resend resolution not applied", changed, err)
	}
	statuses, err := m.Deliver(t.Context(), model.IDFromBytes[Notification](p.ID().Bytes()))
	if err != nil || len(statuses) != 1 || statuses[0].State != Delivered || calls.Load() != 2 {
		t.Fatalf("resolved delivery was not resent: %+v %v", statuses, err)
	}
	if remaining, err := m.Deliveries(t.Context(), Uncertain, 10, DeliveryID{}); err != nil || len(remaining) != 0 {
		t.Fatal("resolved delivery still listed", err)
	}
	// The anchor left Uncertain: continuing from it must not restart silently.
	if _, err := m.Deliveries(t.Context(), Uncertain, 10, listed[0].ID); !errors.Is(err, ErrDeliveryAnchorMoved) {
		t.Fatal("moved anchor restarted the listing", err)
	}
	if page, err := m.Deliveries(t.Context(), Delivered, 10, listed[0].ID); err != nil || len(page) != 0 {
		t.Fatal("anchor continuation in its current state", err)
	}
}

func TestNotificationInboxMarkAllReadAndDeleteStayOwnerScoped(t *testing.T) {
	a := newAuthority(t)
	b := Bind(Define("digest", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel())
	m, _ := fixture(t, b.Registration())
	var first PendingNotification[Member, Input]
	for index, text := range []string{"one", "two", "three"} {
		p := captureNotification(t, b, 1, text)
		if _, err := p.Send(t.Context(), m); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = p
		}
	}
	inbox, err := a.recipient.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	owner, other := a.context(t, 1), a.context(t, 2)
	if changed, err := inbox.MarkAllRead(other); err != nil || changed != 0 {
		t.Fatal("foreign recipient marked records", changed, err)
	}
	if changed, err := inbox.MarkAllRead(owner); err != nil || changed != 3 {
		t.Fatal("mark all read", changed, err)
	}
	if count, err := inbox.UnreadCount(owner); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if changed, err := inbox.MarkAllRead(owner); err != nil || changed != 0 {
		t.Fatal("already-read records changed again", changed, err)
	}
	if deleted, err := inbox.Delete(other, first.ID()); err != nil || deleted {
		t.Fatal("foreign recipient deleted a record", err)
	}
	if deleted, err := inbox.Delete(owner, first.ID()); err != nil || !deleted {
		t.Fatal("owner could not delete", err)
	}
	if deleted, err := inbox.Delete(owner, first.ID()); err != nil || deleted {
		t.Fatal("deleted record removed twice", err)
	}
	page, err := inbox.List(owner, query.PageRequest{Number: 1, Size: 10}, false)
	if err != nil || page.Total != 2 {
		t.Fatal("inbox after delete", page.Total, err)
	}
	// A retry of the delivered notification never recreates the record.
	if _, err := first.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if page, err := inbox.List(owner, query.PageRequest{Number: 1, Size: 10}, false); err != nil || page.Total != 2 {
		t.Fatal("deleted record was recreated", err)
	}
}
