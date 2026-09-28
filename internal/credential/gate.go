package credential

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func IsNil(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return r.IsNil()
	}
	return false
}

// Gate owns callback capacity until actual exit, including cancellation/Goexit.
// It does not own the borrowed backend or retry uncertain operations.
type Gate struct {
	slots   chan struct{}
	timeout time.Duration
}

func NewGate(maximum int, timeout time.Duration) *Gate {
	return &Gate{slots: make(chan struct{}, maximum), timeout: timeout}
}
func (g *Gate) Execute(ctx context.Context, fn func(context.Context) error) error {
	if g == nil || g.slots == nil || ctx == nil || fn == nil {
		return fault.New(fault.Invalid, "credential operation requires a bound store and context")
	}
	op, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	if err := op.Err(); err != nil {
		return err
	}
	select {
	case g.slots <- struct{}{}:
	default:
		return fault.New(fault.Conflict, "credential operation capacity reached")
	}
	defer func() { <-g.slots }()
	err := callback.Isolated("credential operation", func() error {
		if err := op.Err(); err != nil {
			return err
		}
		return fn(op)
	})
	if canceled := op.Err(); canceled != nil {
		// A late cancellation must not erase a known committed/uncertain database
		// outcome or another operational cause. Keep both identities, safely formatted.
		if err != nil {
			return fault.Wrap(fault.Internal, "credential operation canceled", errors.Join(canceled, err))
		}
		return canceled
	}
	if err != nil {
		return fault.Wrap(fault.Internal, "credential operation failed", err)
	}
	return nil
}
