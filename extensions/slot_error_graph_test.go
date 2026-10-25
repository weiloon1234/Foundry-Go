package extensions

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestLoadInPartsDoesNotRetryIncompleteInspection(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		calls := 0
		result, err := LoadInParts(t.Context(), []int{1, 2}, 2, errors.New("limit"), func(context.Context, []int) ([]int, error) { calls++; return nil, cause })
		if err != cause || result != nil || calls != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("failed loader became a retry or lost identity")
		}
	}
}
