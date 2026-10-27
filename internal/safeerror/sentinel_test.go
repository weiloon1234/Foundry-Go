package safeerror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

var diagnosticSentinel = safeerror.Sentinel(fault.Conflict, "owned sentinel")

func TestSentinelSnapshotPreservesPublicTypeIdentityAndImmutableText(t *testing.T) {
	original := *diagnosticSentinel
	defer func() { *diagnosticSentinel = original }()
	for _, exit := range []bool{false, true} {
		t.Run(fmt.Sprint(exit), func(t *testing.T) {
			cause := &hostileDiagnosticCause{exit: exit}
			*diagnosticSentinel = *fault.Wrap(fault.Conflict, "private replacement", cause)
			for _, boundary := range []func(error) error{
				func(e error) error { return callback.Isolated("operation", func() error { return e }) },
				func(e error) error { return safeerror.WrapFailure(fault.Conflict, "operation failed", e) },
			} {
				result := boundary(diagnosticSentinel)
				message, _ := safeerror.OwnedMessage(result)
				if message != "operation: conflict: owned sentinel" && message != "conflict: owned sentinel" {
					t.Fatal("lost sentinel snapshot")
				}
				var retained *fault.Error
				if !errors.Is(result, diagnosticSentinel) || !errors.Is(result, fault.Conflict) || !errors.Is(result, cause) || !errors.As(result, &retained) {
					t.Fatal("lost sentinel identity")
				}
				notes := errordiag.Describe(result).Faults
				found := false
				for _, note := range notes {
					if note.Message == "owned sentinel" {
						found = true
					}
					if note.Message == "private replacement" {
						t.Fatal("mutable sentinel logged")
					}
				}
				if !found {
					t.Fatal("source snapshot missing")
				}
				_ = fmt.Sprintf("%v %+v %#v", result, result, result)
				if cause.calls.Load() != 0 {
					t.Fatal("sentinel cause formatted")
				}
			}
		})
	}
}
