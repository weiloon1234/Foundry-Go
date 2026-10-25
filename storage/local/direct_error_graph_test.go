package local_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"testing"
)

func TestDirectPutContainsSourceErrorInspection(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		_, backend := openLocal(t, t.TempDir())
		cause := &errorgraphtest.Error{Mode: mode}
		_, err := backend.Put(t.Context(), key(t, "failed"), cause, storage.PutOptions{})
		var detail *storage.Error
		if !errors.As(err, &detail) || detail.Outcome() != storage.Unchanged || !errors.Is(err, cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
			t.Fatal("direct backend lost failure outcome or identity")
		}
	}
}
