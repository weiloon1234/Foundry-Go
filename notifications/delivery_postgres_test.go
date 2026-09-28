package notifications

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestNotificationPartialRetryPreservesInboxAndRenderedOutput(t *testing.T) {
	a := newAuthority(t)
	d := Define("orders.placed", 1, textSchema[Input]())
	inboxChannel := databaseChannel()
	var renders, calls atomic.Int32
	var firstKey DeliveryID
	custom := Custom("custom", textSchema[InboxData](), func(_ context.Context, m Member, _ DeliveryContext, p Input) (InboxData, error) {
		renders.Add(1)
		return InboxData{m.Email + ":" + p.Text}, nil
	}, transportFunc[InboxData](func(_ context.Context, key DeliveryID, p InboxData) (Outcome, error) {
		if p.Text != "one@example.test:original" {
			t.Error("prepared route changed")
		}
		if calls.Add(1) == 1 {
			firstKey = key
			return Retry, nil
		}
		if key != firstKey {
			t.Error("delivery key changed")
		}
		return Accepted, nil
	}))
	b := Bind(d, a.recipient, inboxChannel.Channel(), custom)
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	first, err := p.Send(t.Context(), m)
	if err == nil || !first.Retryable() || first.Channels[0].State != Delivered || first.Channels[1].State != Prepared {
		t.Fatal("partial status lost", first, err)
	}
	a.set(Member{ID: 1, Enabled: true, Allowed: true, Email: "new@example.test"})
	second, err := p.Send(t.Context(), m)
	if err != nil || second.Retryable() || calls.Load() != 2 || renders.Load() != 1 {
		t.Fatal("retry rerendered or redelivered", second, err)
	}
	if second.Channels[0].Attempts != 1 || second.Channels[1].Attempts != 2 {
		t.Fatal("per-channel attempts changed")
	}
	inbox, err := a.recipient.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := a.context(t, 1)
	page, err := inbox.List(ctx, query.PageRequest{Number: 1, Size: 10}, true)
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal("inbox duplicated", err)
	}
	decoded, err := inboxChannel.Decode(ctx, d, page.Items[0])
	if err != nil || decoded.Text != "original" {
		t.Fatal(err)
	}
	if _, err := p.Send(t.Context(), m); err != nil || calls.Load() != 2 {
		t.Fatal("completed transport ran again", err)
	}
	changed, err := b.Capture(t.Context(), Member{ID: 1}.FoundryReference(), Input{"changed"}, p.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.Send(t.Context(), m); !errors.Is(err, fault.Conflict) {
		t.Fatal("same ID replaced input", err)
	}
}

func TestNotificationInboxOwnershipReadUnreadAndFreshEligibility(t *testing.T) {
	a := newAuthority(t)
	d := Define("notice", 1, textSchema[Input]())
	b := Bind(d, a.recipient, databaseChannel().Channel())
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "private contents")
	if _, err := p.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	inbox, err := a.recipient.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	owner, other := a.context(t, 1), a.context(t, 2)
	if page, err := inbox.List(other, query.PageRequest{Number: 1, Size: 10}, false); err != nil || page.Total != 0 {
		t.Fatal("foreign inbox listed", err)
	}
	if found, err := inbox.MarkRead(other, p.ID()); err != nil || found {
		t.Fatal("foreign notification acknowledged", err)
	}
	if _, err := inbox.Status(other, p.ID()); err == nil {
		t.Fatal("foreign channel status revealed")
	}
	if count, err := inbox.UnreadCount(owner); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if found, err := inbox.MarkRead(owner, p.ID()); err != nil || !found {
		t.Fatal(err)
	}
	page, err := inbox.List(owner, query.PageRequest{Number: 1, Size: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	when := page.Items[0].ReadAt
	if found, err := inbox.MarkRead(owner, p.ID()); err != nil || !found {
		t.Fatal(err)
	}
	again, err := inbox.List(owner, query.PageRequest{Number: 1, Size: 10}, false)
	if err != nil || again.Items[0].ReadAt != when {
		t.Fatal("read timestamp changed", err)
	}
	if found, err := inbox.MarkUnread(owner, p.ID()); err != nil || !found {
		t.Fatal(err)
	}
	if count, err := inbox.UnreadCount(owner); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := inbox.List(t.Context(), query.PageRequest{Number: 1, Size: 10}, false); err == nil {
		t.Fatal("unverified context read inbox")
	}
	a.set(Member{ID: 1, Enabled: false, Allowed: true})
	if _, err := inbox.UnreadCount(owner); err == nil {
		t.Fatal("cached auth scope bypassed fresh eligibility")
	}
	if strings.Contains(fmt.Sprintf("%#v", page.Items[0]), "private contents") {
		t.Fatal("record log exposed data")
	}
}

func TestNotificationPreferencesAndDeletedRecipientsAtDelivery(t *testing.T) {
	for _, mode := range []string{"preference", "disabled", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			a := newAuthority(t)
			d := Define("notice", 1, textSchema[Input]())
			var sent atomic.Int32
			custom := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
				return InboxData{p.Text}, nil
			}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) { sent.Add(1); return Accepted, nil }))
			b := Bind(d, a.recipient, databaseChannel().Channel(), custom)
			m, _ := fixture(t, b.Registration())
			p := captureNotification(t, b, 1, "captured before change")
			expected := Ineligible
			switch mode {
			case "preference":
				a.set(Member{ID: 1, Enabled: true, Allowed: false})
				expected = Skipped
			case "disabled":
				a.set(Member{ID: 1, Enabled: false, Allowed: true})
			case "deleted":
				a.remove(1)
			}
			r, err := p.Send(t.Context(), m)
			if err != nil || sent.Load() != 0 {
				t.Fatal(r, err)
			}
			for _, status := range r.Channels {
				if status.State != expected {
					t.Fatal("wrong terminal reason", status)
				}
			}
			a.set(Member{ID: 1, Enabled: true, Allowed: true})
			if _, err := p.Send(t.Context(), m); err != nil || sent.Load() != 0 {
				t.Fatal("terminal notification revived", err)
			}
		})
	}
}

func TestNotificationRetryRechecksRevocation(t *testing.T) {
	a := newAuthority(t)
	var calls atomic.Int32
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) { calls.Add(1); return Retry, nil }))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	if r, err := p.Send(t.Context(), m); err == nil || !r.Retryable() {
		t.Fatal("retry missing", err)
	}
	a.remove(1)
	if r, err := p.Send(t.Context(), m); err != nil || r.Channels[0].State != Ineligible || calls.Load() != 1 {
		t.Fatal("deleted recipient retried", r, err)
	}
}

func TestNotificationTransportAbnormalExitIsUncertainAndNeverRetried(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			a := newAuthority(t)
			var calls atomic.Int32
			c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
				return InboxData{p.Text}, nil
			}, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) {
				calls.Add(1)
				switch mode {
				case "error":
					return Accepted, errors.New("private provider contents")
				case "panic":
					panic("private provider contents")
				case "goexit":
					runtime.Goexit()
				}
				return Unknown, nil
			}))
			b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
			m, _ := fixture(t, b.Registration())
			p := captureNotification(t, b, 1, "private payload")
			for range 2 {
				r, err := p.Send(t.Context(), m)
				if err == nil || r.Channels[0].State != Uncertain || strings.Contains(err.Error(), "private") {
					t.Fatal("uncertain outcome lost or leaked", err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("uncertain send duplicated")
			}
		})
	}
}

func TestNotificationConcurrentClaimAndShutdownOwnActualCallback(t *testing.T) {
	a := newAuthority(t)
	started, finish := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(finish) }) }
	defer unblock()
	var calls atomic.Int32
	var m *Manager
	c := Custom("custom", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	}, transportFunc[InboxData](func(ctx context.Context, _ DeliveryID, _ InboxData) (Outcome, error) {
		calls.Add(1)
		if err := m.Close(ctx); !errors.Is(err, fault.Cycle) {
			t.Error("callback self-close", err)
		}
		close(started)
		<-finish
		return Accepted, nil
	}))
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, c)
	m, _ = fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	completed := make(chan error, 1)
	go func() { _, err := p.Send(t.Context(), m); completed <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("transport never started")
	}
	if r, err := p.Send(t.Context(), m); err == nil || r.Channels[0].State != Running || calls.Load() != 1 {
		t.Fatal("concurrent delivery was resent", r, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close abandoned callback", err)
	}
	select {
	case <-m.Done():
		t.Fatal("Done closed while callback active")
	default:
	}
	unblock()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal("acceptance after cancellation lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not finish")
	}
	select {
	case <-m.Done():
	case <-time.After(time.Second):
		t.Fatal("manager did not drain")
	}
}

func TestNotificationEmailFreezesRouteAndBodyAcrossKnownRetry(t *testing.T) {
	a := newAuthority(t)
	var calls atomic.Int32
	var first email.IdempotencyKey
	driver := email.DriverFunc(func(_ context.Context, out email.Outbound) (email.Receipt, error) {
		if got := out.Message(); got.To()[0].Mailbox() != "one@example.test" || got.TextBody() != "original" {
			t.Error("email changed during retry")
		}
		if calls.Add(1) == 1 {
			first = out.IdempotencyKey()
			return email.Receipt{}, email.Transient
		}
		if first != out.IdempotencyKey() {
			t.Error("email idempotency key changed")
		}
		return email.Receipt{MessageID: "accepted"}, nil
	})
	mailer, err := email.New(driver, nil, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mailer.Close(context.Background()) })
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	c := Email("email", mailer, func(_ context.Context, m Member, _ DeliveryContext, p Input) (email.Message, error) {
		to, err := email.ParseAddress(m.Email)
		return email.NewMessage(from, "notice", to).Text(p.Text), err
	})
	b := Bind(Define("notice", 1, textSchema[Input]()), a.recipient, databaseChannel().Channel(), c)
	m, _ := fixture(t, b.Registration())
	p := captureNotification(t, b, 1, "original")
	if r, err := p.Send(t.Context(), m); err == nil || !r.Retryable() {
		t.Fatal(err)
	}
	a.set(Member{ID: 1, Enabled: true, Allowed: true, Email: "changed@example.test"})
	if r, err := p.Send(t.Context(), m); err != nil || r.Retryable() || calls.Load() != 2 {
		t.Fatal(r, err)
	}
}
