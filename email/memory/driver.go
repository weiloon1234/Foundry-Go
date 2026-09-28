// Package memory supplies a bounded, opt-in email recorder. It retains private
// message content in process memory and is intended for tests and development.
package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/email"
)

type Driver struct {
	mu       sync.Mutex
	capacity int
	closed   bool
	messages []email.Outbound
}

func New(capacity int) (*Driver, error) {
	if capacity < 1 || capacity > 10000 {
		return nil, email.Construction
	}
	return &Driver{capacity: capacity}, nil
}
func (*Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("memory email driver")) }
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || len(d.messages) >= d.capacity {
		return email.Receipt{}, email.Transient
	}
	d.messages = append(d.messages, out)
	return email.Receipt{}, nil
}
func (d *Driver) Messages() []email.Outbound {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.messages)
}
func (d *Driver) Reset() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.messages)
	d.messages = nil
}
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
