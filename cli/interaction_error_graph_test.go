package cli

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"io"
	"testing"
)

func TestConfirmContainsReaderErrorInspection(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		err := Confirm(t.Context(), Streams{In: cause, Out: io.Discard, Err: io.Discard}, "Continue?", false)
		if err != cause || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("confirmation accepted or reformatted failed input")
		}
	}
}
