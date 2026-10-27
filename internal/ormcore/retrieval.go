package ormcore

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
)

// ReadCallbacks belongs only to a complete-model decoder. Projection and write
// decoders pass nil. Needed inspects the actual returned rows' frozen owner.
type ReadCallbacks struct {
	Needed   func(lifecycle.Observers) bool
	Complete func(context.Context, lifecycle.Observers) error
}

// Collect closes a complete batch before dispatch, retaining the originating
// owner's callback lease across closure. Both model APIs use this lifetime owner.
// Empty, incomplete, canceled or unsuccessfully closed batches skip dispatch.
func Collect(ctx context.Context, rows *database.Rows, scan func(database.Row) error, callbacks *ReadCallbacks) (err error) {
	defer func() { err = safeerror.Join("operation failed", err, rows.Close()) }()
	count := 0
	collect := func(work context.Context) error {
		var err error
		count, err = rows.FoundryCollect(sqlowner.Collections(), work, scan)
		return err
	}
	set := rows.Observers()
	if callbacks == nil || !callbacks.Needed(set) {
		return collect(ctx)
	}
	return rows.WithObserverScope(ctx, func(work context.Context) error {
		if err := collect(work); err != nil || count == 0 {
			return err
		}
		work, err := lifecore.Context(work)
		if err != nil {
			return err
		}
		if err := callbacks.Complete(work, set); err != nil {
			return lifecore.Failure("model retrieved hook failed", err)
		}
		return work.Err()
	})
}
