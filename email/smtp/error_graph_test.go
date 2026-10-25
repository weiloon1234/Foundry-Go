package smtp

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestTLSAndReplyClassifiersContainApplicationErrorMethods(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		for _, classify := range []func(error) error{tlsFailure, beforeData, authFailure} {
			cause := &errorgraphtest.Error{Mode: mode}
			err := classify(cause)
			if !errors.Is(err, email.Transient) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
				t.Fatal("incomplete transport inspection became permanent or escaped containment")
			}
		}
	}
}
