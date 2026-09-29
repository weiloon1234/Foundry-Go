// Package failover composes borrowed email transports. A send moves to the next
// transport only after a Transient failure: a known non-acceptance. Permanent,
// Construction and Ambiguous outcomes stop immediately, because an ambiguous
// submission may already have been accepted and trying another transport could
// deliver the message twice.
package failover

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// MaxTransports bounds one composition.
const MaxTransports = 8

// Driver borrows its transports; close them after the owning mailer drains.
// It renders MIME for every transport, which provider API drivers accept too.
type Driver struct {
	transports []email.Driver
	rotate     bool
	next       atomic.Uint64
}

// New tries transports in order on every send.
func New(transports ...email.Driver) (*Driver, error) { return compose(false, transports) }

// RoundRobin starts each send at the next transport in turn, spreading load,
// and fails over through the others in order.
func RoundRobin(transports ...email.Driver) (*Driver, error) { return compose(true, transports) }

func compose(rotate bool, transports []email.Driver) (*Driver, error) {
	if len(transports) < 2 || len(transports) > MaxTransports {
		return nil, email.Construction
	}
	for i, transport := range transports {
		if transport == nil || isNil(transport) || slices.ContainsFunc(transports[:i], func(other email.Driver) bool { return sameDriver(other, transport) }) {
			return nil, email.Construction
		}
	}
	return &Driver{transports: slices.Clone(transports), rotate: rotate}, nil
}

func isNil(value any) bool {
	r := reflect.ValueOf(value)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// sameDriver rejects composing one pointer transport twice; functions and
// other non-comparable transports are treated as distinct.
func sameDriver(a, b email.Driver) bool {
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Kind() == reflect.Pointer && y.Kind() == reflect.Pointer && x.Type() == y.Type() && x.Pointer() == y.Pointer()
}

func (*Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email failover driver")) }

func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || len(d.transports) == 0 || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	start := 0
	if d.rotate {
		start = int((d.next.Add(1) - 1) % uint64(len(d.transports)))
	}
	var failures []error
	for offset := range d.transports {
		if ctx.Err() != nil {
			return email.Receipt{}, errors.Join(append(failures, email.Transient)...)
		}
		transport := d.transports[(start+offset)%len(d.transports)]
		var receipt email.Receipt
		err := callback.Isolated("send email through failover transport", func() error {
			var err error
			receipt, err = transport.Send(ctx, out)
			return err
		})
		if err == nil {
			return receipt, nil
		}
		if email.Classification(err) != email.Transient {
			return email.Receipt{}, err
		}
		failures = append(failures, err)
	}
	return email.Receipt{}, errors.Join(failures...)
}
