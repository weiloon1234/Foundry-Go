package compilefail

import (
	"context"
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid() {
	var dispatcher *jobs.Dispatcher
	_, _ = background.WelcomeJob.Cancel(context.Background(), dispatcher, jobs.ID[background.RemoveExport]{}, "")
}
