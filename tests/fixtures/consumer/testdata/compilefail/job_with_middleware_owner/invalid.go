package compilefail

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid() {
	_ = application.JobWith(background.WelcomeJob, func(application.Services) (jobs.Handler[background.RemoveExport], jobs.HandlerOptions[background.RemoveExport], error) {
		return nil, jobs.HandlerOptions[background.RemoveExport]{}, nil
	})
}
