// Package memory supplies a bounded, opt-in email recorder. It retains private
// message content in process memory and is intended for tests and development.
package memory

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/email"
)

// Driver retains outbound messages in a bounded, mutex-protected buffer.
type Driver struct {
	mu       sync.Mutex
	capacity int
	closed   bool
	messages []email.Outbound
}

// New creates an in-memory driver with capacity between one and 10,000 messages.
func New(capacity int) (*Driver, error) {
	if capacity < 1 || capacity > 10000 {
		return nil, emaildiag.Failure(email.Construction)
	}
	return &Driver{capacity: capacity}, nil
}

// Format writes a fixed label without exposing messages.
func (*Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("memory email driver")) }

// Send retains a validated outbound message, returning a transient failure if closed or full.
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, emaildiag.Failure(email.Construction)
	}
	if ctx.Err() != nil {
		return email.Receipt{}, emaildiag.Failure(email.Transient)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || len(d.messages) >= d.capacity {
		return email.Receipt{}, emaildiag.Failure(email.Transient)
	}
	d.messages = append(d.messages, out)
	return email.Receipt{}, nil
}

// Messages returns a synchronized copy of the retained message slice.
func (d *Driver) Messages() []email.Outbound {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.messages)
}

// Reset clears retained messages without reopening a closed driver.
func (d *Driver) Reset() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.messages)
	d.messages = nil
}

// Close closes the driver and clears retained messages under its mutex.
func (d *Driver) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	clear(d.messages)
	d.messages = nil
}
