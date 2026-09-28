package compilefail

import (
	"context"
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

func invalid(dispatcher *jobs.Dispatcher) {
	_, _ = schedule.JobTarget(dispatcher, background.WelcomeJob, func(context.Context, schedule.Invocation) (background.RemoveExport, error) {
		return background.RemoveExport{}, nil
	})
}
