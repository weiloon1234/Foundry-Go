package jobs

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestWorkerCancellationClassificationStaysBounded(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		result := safeExecutionResult(Policy{Attempts: 1}, 1, 0, errors.New("handler failed"), cause, false)
		if result.State != Failed || result.Reason != HandlerFailed || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("unknown cancellation changed worker outcome")
		}
	}
}
