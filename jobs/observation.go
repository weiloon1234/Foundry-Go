package jobs

import "github.com/weiloon1234/Foundry-Go/observability"

func jobOutcome(result Result) observability.Outcome {
	switch result.Reason {
	case HandlerPanicked:
		return observability.Panicked
	case TimedOut:
		return observability.TimedOut
	case WorkerStopped:
		if result.State == Failed {
			return observability.Failed
		}
		return observability.Cancelled
	case CancelRequested:
		return observability.Cancelled
	case RateLimited:
		return observability.Rejected
	}
	if result.State == Succeeded {
		return observability.Succeeded
	}
	return observability.Failed
}
