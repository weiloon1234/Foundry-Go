package compilefail

import (
	"context"
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid() {
	var dispatcher *jobs.Dispatcher
	_, _ = background.WelcomeJob.Dispatch(context.Background(), dispatcher, background.Welcome{}, jobs.Options[background.RemoveExport]{})
}
