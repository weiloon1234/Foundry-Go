// Package notifications supplies a recording custom-channel transport for
// tests. Bind it with notifications.Custom so tests exercise the production
// manager, storage and claim protocol; only the external send is replaced.
// Assertion failure text never includes rendered outputs or delivery IDs.
package notifications

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

type TestingT interface {
	Helper()
	Errorf(string, ...any)
}

// Delivery is one recorded send.
type Delivery[D any] struct {
	ID   notifications.DeliveryID
	Data D
}

// Transport records delivered outputs in arrival order and answers with the
// configured outcome (Accepted by default). It is bounded by its capacity.
type Transport[D any] struct {
	mu        sync.Mutex
	capacity  int
	outcome   notifications.Outcome
	delivered []Delivery[D]
}

var _ notifications.Transport[struct{}] = (*Transport[struct{}])(nil)

// New owns a recorder whose private outputs are cleared when the test ends.
func New[D any](t testing.TB, capacity int) *Transport[D] {
	t.Helper()
	if capacity < 1 || capacity > 10000 {
		t.Fatal("invalid notification transport capacity")
	}
	transport := &Transport[D]{capacity: capacity, outcome: notifications.Accepted}
	t.Cleanup(transport.Reset)
	return transport
}

// Respond selects the outcome for later deliveries, for example Retry to
// exercise a retry or Unknown to exercise an operator resolution.
func (r *Transport[D]) Respond(outcome notifications.Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcome = outcome
}

func (r *Transport[D]) Deliver(ctx context.Context, id notifications.DeliveryID, data D) (notifications.Outcome, error) {
	if r == nil || ctx == nil {
		return notifications.Reject, fault.New(fault.Invalid, "uninitialized notification test transport")
	}
	if err := ctx.Err(); err != nil {
		return notifications.Retry, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.delivered) >= r.capacity {
		return notifications.Retry, nil
	}
	if r.outcome == notifications.Accepted || r.outcome == notifications.Unknown {
		r.delivered = append(r.delivered, Delivery[D]{ID: id, Data: data})
	}
	return r.outcome, nil
}

// Deliveries returns a copy of the accepted (or unknown-outcome) sends.
func (r *Transport[D]) Deliveries() []Delivery[D] {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.delivered)
}

// Reset clears recorded outputs.
func (r *Transport[D]) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.delivered)
	r.delivered = nil
}

func countDelivered[D any](r *Transport[D], match func(D) bool) int {
	got := 0
	for _, delivery := range r.Deliveries() {
		if match == nil || match(delivery.Data) {
			got++
		}
	}
	return got
}

// AssertDelivered requires at least one recorded output satisfying match (nil
// matches any).
func AssertDelivered[D any](t TestingT, r *Transport[D], match func(D) bool) {
	t.Helper()
	if countDelivered(r, match) == 0 {
		t.Errorf("no matching notification was delivered")
	}
}

// AssertNotDelivered requires that no recorded output satisfies match.
func AssertNotDelivered[D any](t TestingT, r *Transport[D], match func(D) bool) {
	t.Helper()
	if got := countDelivered(r, match); got != 0 {
		t.Errorf("%d matching notification(s) were delivered", got)
	}
}

// AssertDeliveredCount requires exactly want recorded outputs satisfying match.
func AssertDeliveredCount[D any](t TestingT, r *Transport[D], match func(D) bool, want int) {
	t.Helper()
	if got := countDelivered(r, match); got != want {
		t.Errorf("matching notification count: got %d, want %d", got, want)
	}
}
