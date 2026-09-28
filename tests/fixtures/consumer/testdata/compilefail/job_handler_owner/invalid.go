package compilefail

import (
	"context"
	"foundry.test/consumer/background"
)

func invalid() {
	_, _ = background.WelcomeJob.Declare(func(context.Context, background.RemoveExport) error { return nil })
}
