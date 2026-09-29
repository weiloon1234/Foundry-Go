package memory

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

var _ jobs.RetryBackend = (*Backend)(nil)

func (b *Backend) JobRetry(ctx context.Context, key jobs.Key, request jobs.RetryRequest) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	now, release, err := b.begin(ctx, key)
	if err != nil {
		return false, err
	}
	defer release()
	item, ok := b.entries[address{key, request.Target.ID}]
	if !ok {
		return false, jobs.ErrNotRetryable
	}
	already, err := jobs.CheckRetry(item.record, request)
	if err != nil || already {
		return false, err
	}
	item.record.Retries++
	item.record.LastRetry = request.Token
	item.record.Attempts = 0
	item.record.Exceptions = 0
	item.record.FinishedAt = time.Time{}
	item.record.AvailableAt = now
	item.abandoned = 0
	b.transition(item, jobs.Waiting, jobs.ManuallyRetried, now)
	b.signal(key)
	return true, nil
}
