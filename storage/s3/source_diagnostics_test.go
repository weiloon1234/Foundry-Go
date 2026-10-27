package s3

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type privateS3SourceDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateS3SourceDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private S3 diagnostic")
}
func (e *privateS3SourceDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestS3SourceCodeDiagnosticsSurviveContainment(t *testing.T) {
	for _, test := range []struct {
		name string
		code storage.Code
		run  func() error
	}{
		{"nil locator", storage.Invalid, func() error { _, err := (*Backend)(nil).Locate(storage.ObjectKey{}); return err }},
		{"unprepared locator", storage.Invalid, func() error { _, err := (&Backend{}).Locate(storage.ObjectKey{}); return err }},
		{"invalid cursor", storage.Invalid, func() error { var value struct{}; return decodeCursor(storage.NewCursor("!"), &value) }},
		{"large cursor", storage.LimitExceeded, func() error { _, err := encodeCursor(strings.Repeat("x", storage.MaxCursorBytes)); return err }},
		{"content range", storage.IntegrityFailed, func() error { _, _, _, err := parseContentRange("private response"); return err }},
		{"upload reference JSON", storage.Invalid, func() error { _, err := (UploadReference{}).MarshalJSON(); return err }},
		{"upload cleanup reference", storage.Invalid, func() error { _, err := (&Backend{prepared: true}).UploadFromCleanup(storage.CleanupID{}); return err }},
	} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", test.name, exit), func(t *testing.T) {
				original := test.run()
				result := callback.Isolated("consumer", func() error { return original })
				want := "consumer: " + string(test.code)
				var retained storage.Code
				if result == nil || result.Error() != want || !errors.Is(result, original) || !errors.Is(result, test.code) || !errors.As(result, &retained) || retained != test.code {
					t.Fatal("S3 source diagnostic, classification or identity was lost")
				}
				cause := &privateS3SourceDiagnostic{exit: exit}
				application := errors.Join(test.code, cause)
				redacted := callback.Isolated("consumer", func() error { return application })
				var private *privateS3SourceDiagnostic
				if redacted.Error() != "consumer failed" || !errors.Is(redacted, application) || !errors.Is(redacted, test.code) || !errors.Is(redacted, cause) || !errors.As(redacted, &private) || private != cause {
					t.Fatal("application classification acquired diagnostic ownership or lost its cause")
				}
				if fmt.Sprintf("%v %+v %#v", redacted, redacted, redacted) != "consumer failed consumer failed consumer failed" || cause.calls.Load() != 0 {
					t.Fatal("S3 source containment formatted application text")
				}
			})
		}
	}
}
