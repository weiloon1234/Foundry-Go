package credential

import (
	"context"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
// It does not own the borrowed backend or retry uncertain operations. A burst
// queues for at most admission.Wait(timeout); an unsatisfied wait reports
// fault.Overloaded before the operation starts.
type Gate struct {
	slots   *admission.Semaphore
	timeout time.Duration
}

func NewGate(maximum int, timeout time.Duration) *Gate {
	return &Gate{slots: admission.New(maximum), timeout: timeout}
}
func (g *Gate) Execute(ctx context.Context, fn func(context.Context) error) error {
	if g == nil || g.slots == nil || ctx == nil || fn == nil {
		return safeerror.NewFault(fault.Invalid, "credential operation requires a bound store and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Queue before the operation budget starts, so waiting cannot consume it.
	if err := g.slots.Acquire(ctx, admission.Wait(g.timeout), nil); err != nil {
		return err
	}
	defer g.slots.Release()
	op, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	err := callback.Isolated("credential operation", func() error {
		if err := op.Err(); err != nil {
			return err
		}
		return fn(op)
	})
	if err == nil {
		// The callback completed; a deadline reached afterwards cannot undo a
		// committed result, so success is reported rather than discarded.
		return nil
	}
	if canceled := op.Err(); canceled != nil {
		// A late cancellation must not erase a known committed/uncertain database
		// outcome or another operational cause. Keep both identities, safely formatted.
		return safeerror.WrapFault(operationFailureCode(err), "credential operation canceled", safeerror.Join("operation failed", canceled, err))
	}
	return safeerror.WrapFailure(operationFailureCode(err), "credential operation failed", err)
}
