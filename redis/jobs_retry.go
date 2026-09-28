package redis

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

var _ jobs.RetryBackend = (*JobBackend)(nil)

func (b *JobBackend) JobRetry(ctx context.Context, key jobs.Key, request jobs.RetryRequest) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	found, err := b.JobInspect(ctx, key, request.Target.ID)
	if err != nil {
		return false, err
	}
	record, ok := found.Get()
	if !ok {
		return false, jobs.ErrNotRetryable
	}
	already, err := jobs.CheckRetry(record, request)
	if err != nil || already {
		return false, err
	}
	encoded, err := record.Envelope.MarshalJSON()
	if err != nil {
		return false, err
	}
	// Read only to calculate the shared fingerprint. The script compares every
	// fingerprint input under the queue authority before changing any state.
	reply, err := b.command(ctx, key, jobRequest{
		Op: "retry", ID: request.Target.ID.String(), Name: request.Target.Name, Version: request.Target.Version,
		RetryToken: request.Token, Envelope: string(encoded), ExpectedCreated: record.CreatedAt.UnixMilli(),
		ExpectedFinished: record.FinishedAt.UnixMilli(), ExpectedAttempts: record.Attempts,
		ExpectedRetries: record.Retries, MaxRetries: jobs.MaxManualRetries,
	})
	return reply.Changed, err
}
