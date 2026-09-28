package compilefail

import (
	"context"
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid(dispatcher *jobs.Dispatcher, token jobs.RetryToken) {
	_, _ = background.WelcomeJob.Retry(context.Background(), dispatcher, jobs.ID[background.RemoveExport]{}, "", token)
}
