package sqlcallback

import (
	"context"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RowBatch lends Next containment to a complete collection. The database owner
// must hold its stream mutex throughout Run, including callback exit and cleanup.
// ScanColumn and Close never borrow it: foreign scan/close frames need their own
// Goexit containment to release locks before returning to the collection owner.
type RowBatch struct {
	active    atomic.Bool
	operation string
}

type rowBatchKey struct{}

// WithRowBatch delivers a private lease to the connector wrapper. It is removed
// before calling an application driver, so nested/retained driver contexts cannot
// borrow another stream's containment.
func WithRowBatch(ctx context.Context) (context.Context, *RowBatch) {
	batch := new(RowBatch)
	return context.WithValue(ctx, rowBatchKey{}, batch), batch
}

func rowBatch(ctx context.Context) *RowBatch {
	batch, _ := ctx.Value(rowBatchKey{}).(*RowBatch)
	return batch
}

func driverContext(ctx context.Context) context.Context {
	if rowBatch(ctx) != nil {
		return context.WithValue(ctx, rowBatchKey{}, nil)
	}
	return ctx
}

// Run contains the whole sequential iteration. A driver Goexit retains the
// same diagnostic as an individually isolated Next, and is identified separately
// so the stream owner can classify its outcome before closing rows.
func (b *RowBatch) Run(operation string, fn func() error) (err error, driverExit bool) {
	returned := false
	err = callback.Isolated(operation, func() error {
		b.active.Store(true)
		defer b.active.Store(false)
		failure := fn()
		returned = true
		return failure
	})
	if !returned && b.operation != "" {
		return safeerror.Seal(b.operation+" failed", safeerror.NewFault(fault.Panicked, b.operation+" exited without returning")), true
	}
	return err, false
}

func (b *RowBatch) call(operation string, fn func() error) error {
	b.operation = operation
	var original error
	contained := callback.Invoke(operation, func() error { original = fn(); return nil })
	b.operation = ""
	return driverError(operation, original, contained)
}
