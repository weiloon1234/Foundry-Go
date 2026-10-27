package attachments

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestAttachmentDiagnosticSurvivesCleanupAndMutation(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(fmt.Sprint(exit), func(t *testing.T) {
			marked := invalid()
			var original *fault.Error
			if !errors.As(marked, &original) {
				t.Fatal("missing input fault")
			}
			cause := &privateAttachmentError{exit: exit}
			*original = *fault.Wrap(fault.Invalid, "private replacement", cause)
			if joinFailures(nil, marked, nil) != marked {
				t.Fatal("empty cleanup changed the original identity")
			}
			joined := joinFailures(marked, cause)
			err := callback.Isolated("attachment operation", func() error { return joined })
			var retained *privateAttachmentError
			var retainedFault *fault.Error
			if !errors.Is(err, marked) || !errors.Is(err, fault.Invalid) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause || !errors.As(err, &retainedFault) || retainedFault != original {
				t.Fatal("cleanup lost its failure graph")
			}
			want := "attachment operation: invalid: invalid attachment collection, operation or data\nattachment operation failed"
			if err.Error() != want || strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.calls.Load() != 0 {
				t.Fatal("cleanup formatted or replaced the owned diagnostic")
			}
		})
	}
}
