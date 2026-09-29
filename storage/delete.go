package storage

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// MaxBatchDelete bounds one DeleteMany or DeletePrefix page.
const MaxBatchDelete = 1000

// BatchDeleter deletes several keys unconditionally in one provider request.
// Results align with keys; nil means deleted or already absent. A call-level
// error means the batch outcome is unknown for every key without a result.
type BatchDeleter interface {
	DeleteBatch(ctx context.Context, keys []ObjectKey) ([]error, error)
}

// DeleteFailure is one key a batch did not delete; Err is a classified
// *Error whose Outcome distinguishes unchanged from uncertain deletion.
type DeleteFailure struct {
	Key ObjectKey
	Err error
}

type DeleteManyResult struct {
	Deleted []ObjectKey
	Failed  []DeleteFailure
}

// DeleteMany unconditionally deletes up to MaxBatchDelete distinct keys under
// one operation slot, using BatchDeleter when the adapter provides it. It is
// idempotent for absent keys and never implies a transaction: inspect Failed.
// A call-level error reports keys whose outcome is unknown as Failed.
func (d *Disk) DeleteMany(ctx context.Context, keys []ObjectKey) (DeleteManyResult, error) {
	if err := d.Validate(); err != nil {
		return DeleteManyResult{}, err
	}
	if len(keys) == 0 || len(keys) > MaxBatchDelete {
		return DeleteManyResult{}, Failure(Invalid, DeleteOperation, Unchanged, nil)
	}
	owned := make([]ObjectKey, len(keys))
	seen := make(map[ObjectKey]bool, len(keys))
	for i, key := range keys {
		if err := key.Validate(); err != nil {
			return DeleteManyResult{}, err
		}
		if seen[key] {
			return DeleteManyResult{}, Failure(Invalid, DeleteOperation, Unchanged, nil)
		}
		seen[key], owned[i] = true, key
	}
	op, release, err := d.begin(ctx, DeleteOperation)
	if err != nil {
		return DeleteManyResult{}, err
	}
	defer release()
	results := make([]error, len(owned))
	if batch, ok := d.backend.(BatchDeleter); ok {
		var returned []error
		err = callback.Isolated("storage batch delete", func() error { var err error; returned, err = batch.DeleteBatch(op, owned); return err })
		if err == nil && len(returned) != len(owned) {
			err = Failure(IntegrityFailed, DeleteOperation, Unknown, nil)
		}
		if err != nil {
			err = finish(DeleteOperation, op, err, Applied)
			for i := range results {
				results[i] = err
			}
		} else {
			copy(results, returned)
		}
	} else {
		for i, key := range owned {
			if op.Err() != nil {
				results[i] = finish(DeleteOperation, op, nil, Unchanged)
				continue
			}
			results[i] = callback.Isolated("storage delete", func() error { return d.backend.Delete(op, key, DeleteOptions{}) })
		}
	}
	var result DeleteManyResult
	var failures error
	for i, key := range owned {
		failed := results[i]
		if failed != nil {
			failed = finish(DeleteOperation, op, failed, Applied)
		}
		if failed == nil {
			result.Deleted = append(result.Deleted, key)
			continue
		}
		result.Failed = append(result.Failed, DeleteFailure{Key: key, Err: failed})
		failures = errors.Join(failures, failed)
	}
	if failures != nil {
		return result, Failure(Unavailable, DeleteOperation, mutationOutcome(result.Failed[0].Err), failures)
	}
	return result, nil
}

// DeletePrefixOptions selects one bounded page below a nonempty prefix.
type DeletePrefixOptions struct {
	Prefix Prefix
	Cursor Cursor
	Limit  int
}

// DeletePrefixResult reports one page. Skipped entries (foreign or oversized
// objects) are never deleted. Continue with Next until it is zero.
type DeletePrefixResult struct {
	DeleteManyResult
	Skipped int
	Next    Cursor
}

// DeletePrefix lists one page under a nonempty prefix and deletes its objects
// unconditionally. It is not a snapshot: objects written concurrently can
// survive, and a directory is never implied to be empty afterwards.
func (d *Disk) DeletePrefix(ctx context.Context, options DeletePrefixOptions) (DeletePrefixResult, error) {
	if options.Prefix.String() == "" || options.Limit < 1 || options.Limit > MaxBatchDelete {
		return DeletePrefixResult{}, Failure(Invalid, DeleteOperation, Unchanged, nil)
	}
	page, err := d.List(ctx, ListOptions{Prefix: options.Prefix, Cursor: options.Cursor, Limit: options.Limit})
	if err != nil {
		return DeletePrefixResult{}, err
	}
	result := DeletePrefixResult{Skipped: page.Skipped, Next: page.Next}
	if len(page.Objects) == 0 {
		return result, nil
	}
	keys := make([]ObjectKey, len(page.Objects))
	for i, info := range page.Objects {
		keys[i] = info.Key
	}
	result.DeleteManyResult, err = d.DeleteMany(ctx, keys)
	return result, err
}
