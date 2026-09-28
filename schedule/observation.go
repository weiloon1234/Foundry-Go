package schedule

import "github.com/weiloon1234/Foundry-Go/observability"

func scheduleOutcome(state State, reason Reason) observability.Outcome {
	switch reason {
	case Panicked:
		return observability.Panicked
	case TimedOut:
		return observability.TimedOut
	}
	switch state {
	case Succeeded:
		return observability.Succeeded
	case Cancelled:
		return observability.Cancelled
	case Skipped:
		return observability.Rejected
	default:
		return observability.Failed
	}
}
