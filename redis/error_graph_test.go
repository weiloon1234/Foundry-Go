package redis

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestDriverClassificationContainsRetainedMethods(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		if err := classify(t.Context(), cause); err == nil || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
			t.Fatal("Redis classifier escaped error bounds")
		}
	}
}
