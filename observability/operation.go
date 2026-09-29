package observability

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Observe runs work synchronously with the recorder attached to ctx. A missing,
// closed or exhausted recorder does not prevent domain work; drop counters
// remain available on the recorder. Existing callback isolation still belongs
// to the caller. The deferred end also releases ownership on panic or Goexit.
// The returned error alone decides the outcome: work that returned nil stays
// Succeeded even if its context is cancelled or expires afterwards.
func Observe(ctx context.Context, operation Operation, run func(context.Context) error) (err error) {
	if ctx == nil || run == nil {
		return fault.New(fault.Invalid, "observation requires a context and callback")
	}
	recorder := FromContext(ctx)
	if recorder == nil {
		return run(ctx)
	}
	work, span, _ := recorder.Start(ctx, operation)
	if work == nil {
		work = ctx
	}
	err = fault.New(fault.Panicked, "observed callback exited without returning")
	defer func() { span.End(Result{Outcome: OutcomeFor(err)}) }()
	err = run(work)
	return err
}
