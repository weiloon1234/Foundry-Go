package background

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

// doc-example:begin jobs-operations-1
func inspectAndRetryWelcome(ctx context.Context, dispatcher *jobs.Dispatcher, id jobs.ID[Welcome], WelcomeJob jobs.Definition[Welcome]) error {
	found, err := WelcomeJob.Inspect(ctx, dispatcher, id, "communications")
	if err != nil {
		return err
	}
	record, present := found.Get()
	if !present {
		return jobs.ErrNotRetryable
	}
	token, err := record.RetryToken()
	if err != nil {
		return err
	}
	// Save token before executing the operator-approved action.
	changed, err := WelcomeJob.Retry(ctx, dispatcher, id, "communications", token)
	if err != nil {
		return err
	}
	_ = changed
	return err
}

// doc-example:end jobs-operations-1
