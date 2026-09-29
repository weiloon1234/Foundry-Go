package jobs

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// QueueStats is safe operational depth for one queue. Waiting jobs are eligible
// now; Delayed wait for a future availability; Blocked are workflow members
// awaiting predecessors; Leased are reserved or running. Retained counts
// terminal records kept for deduplication and inspection, including Failed.
type QueueStats struct {
	Waiting  int64 `json:"waiting"`
	Delayed  int64 `json:"delayed"`
	Blocked  int64 `json:"blocked"`
	Leased   int64 `json:"leased"`
	Failed   int64 `json:"failed"`
	Retained int64 `json:"retained"`
}

// StatsBackend is optional; built-in memory and Redis authorities implement it
// without reading payloads.
type StatsBackend interface {
	JobStats(context.Context, Key) (QueueStats, error)
}

// ForgetBackend is optional. It removes one retained terminal record that is
// not a workflow member, including its deduplication identity: a later dispatch
// or outbox republication of the same ID is then accepted as new work.
type ForgetBackend interface {
	JobForget(context.Context, Key, Target) (bool, error)
}

// Stats reports queue depth through the optional StatsBackend.
func (d *Dispatcher) Stats(ctx context.Context, queue Queue) (QueueStats, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return QueueStats{}, err
	}
	defer release()
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return QueueStats{}, err
	}
	backend, ok := d.backend.(StatsBackend)
	if !ok {
		return QueueStats{}, fault.New(fault.Invalid, "job backend does not report queue statistics")
	}
	return backend.JobStats(ctx, key)
}

// Forget removes one retained terminal independent job. Live and workflow jobs
// are never removed; changed=false means nothing matching was retained.
func (d *Dispatcher) Forget(ctx context.Context, queue Queue, target Target) (bool, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if err := target.Validate(); err != nil {
		return false, err
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return false, err
	}
	backend, ok := d.backend.(ForgetBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "job backend does not support forgetting records")
	}
	return backend.JobForget(ctx, key, target)
}

// BulkResult reports one bounded operator page. Changed counts confirmed
// mutations; Next continues the scan and may be nonzero after an empty page.
// A returned error leaves earlier confirmed changes in place.
type BulkResult struct {
	Matched int         `json:"matched"`
	Changed int         `json:"changed"`
	Next    ExecutionID `json:"next,omitzero"`
}

// RetryFailed explicitly retries each independent failed job in one bounded
// page (options.State is forced to Failed). Each retry uses the token of the
// observed failure, so a concurrent change is skipped, never overwritten.
// Workflow members, jobs at their manual retry cap and names/versions this
// dispatcher does not register are skipped.
func (d *Dispatcher) RetryFailed(ctx context.Context, queue Queue, options ListOptions) (BulkResult, error) {
	options.State = Failed
	return d.bulk(ctx, queue, options, func(record Record) (bool, error) {
		token, err := record.RetryToken()
		if err != nil {
			return false, nil
		}
		if _, err := d.registry.lookup(jobKey{record.Envelope.Name(), record.Envelope.Version()}); err != nil {
			return false, nil
		}
		changed, err := d.Retry(ctx, queue, RetryRequest{Target: record.Envelope.Target(), Token: token})
		if errors.Is(err, ErrNotRetryable) {
			return false, nil
		}
		return changed, err
	})
}

// FlushFailed forgets each independent failed job in one bounded page.
func (d *Dispatcher) FlushFailed(ctx context.Context, queue Queue, options ListOptions) (BulkResult, error) {
	options.State = Failed
	return d.bulk(ctx, queue, options, func(record Record) (bool, error) {
		if !record.Workflow.IsZero() {
			return false, nil
		}
		return d.Forget(ctx, queue, record.Envelope.Target())
	})
}

// Clear requests cancellation of every unfinished job in one bounded page.
// Waiting and blocked jobs end as cancelled immediately; running handlers
// receive cancellation and keep ownership while draining. Records and history
// are retained, so cleared work remains inspectable and deduplicated.
func (d *Dispatcher) Clear(ctx context.Context, queue Queue, options ListOptions) (BulkResult, error) {
	options.State = ""
	return d.bulk(ctx, queue, options, func(record Record) (bool, error) {
		if record.State.Terminal() || record.CancellationRequested {
			return false, nil
		}
		key, err := NewKey(d.config.Namespace, queue)
		if err != nil {
			return false, err
		}
		release, err := d.begin(ctx)
		if err != nil {
			return false, err
		}
		defer release()
		return d.backend.JobCancel(ctx, key, record.Envelope.Target())
	})
}

func (d *Dispatcher) bulk(ctx context.Context, queue Queue, options ListOptions, apply func(Record) (bool, error)) (BulkResult, error) {
	page, err := d.List(ctx, queue, options)
	if err != nil {
		return BulkResult{}, err
	}
	result := BulkResult{Next: page.Next}
	for _, record := range page.Records {
		if !options.Matches(record) {
			continue
		}
		result.Matched++
		changed, err := apply(record)
		if err != nil {
			return result, err
		}
		if changed {
			result.Changed++
		}
	}
	return result, nil
}

// MigrateLayout explicitly migrates queue to the backend's current storage
// layout (see ErrLegacyLayout). Run it only after every process of the
// previous release stopped or drained: they cannot read the migrated queue.
// migrated=false means nothing needed migrating; repeating is safe. A backend
// without layouts reports false.
func (d *Dispatcher) MigrateLayout(ctx context.Context, queue Queue) (bool, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return false, err
	}
	migrator, ok := d.backend.(LayoutMigrator)
	if !ok {
		return false, nil
	}
	return migrator.JobMigrateLayout(ctx, key)
}
