package idempotency

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type hostileDiagnosticCause struct{ formats atomic.Int32 }

func (e *hostileDiagnosticCause) Error() string {
	e.formats.Add(1)
	panic("private idempotency cause")
}
func (e *hostileDiagnosticCause) Format(fmt.State, rune) {
	e.formats.Add(1)
	panic("private idempotency formatter")
}

func TestOwnedIdempotencyDiagnostics(t *testing.T) {
	for _, code := range []Code{BadKey, Mismatch, InProgress, Capacity, Unavailable} {
		t.Run(string(code), func(t *testing.T) {
			cause := &hostileDiagnosticCause{}
			source := failure(code, cause)
			var detail *Error
			if !errors.As(source, &detail) {
				t.Fatal("source lost its typed error")
			}
			got := callback.Isolated("consumer", func() error { return source })
			if got.Error() != "consumer: "+string(code) || !errors.Is(got, code) || !errors.Is(got, source) || !errors.Is(got, detail) || !errors.Is(got, cause) {
				t.Fatal("source diagnostic, classification or identity was lost")
			}
			detail.code = Code("private replacement")
			if got.Error() != "consumer: "+string(code) || cause.formats.Load() != 0 {
				t.Fatal("diagnostic was mutable or formatted its cause")
			}
		})
	}
}

func TestParsedAndRestoredDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name string
		code Code
		run  func(*testing.T) error
	}{
		{"short key", BadKey, func(*testing.T) error { _, err := ParseKey("short"); return err }},
		{"invalid key", BadKey, func(*testing.T) error { _, err := ParseKey("private key invalid"); return err }},
		{"mismatch", Mismatch, func(t *testing.T) error {
			op, row := storedTestOutcome(t)
			_, err := op.restore(t.Context(), row, "different")
			return err
		}},
		{"empty representation", Unavailable, func(t *testing.T) error {
			op, row := storedTestOutcome(t)
			row.Representation = nil
			_, err := op.restore(t.Context(), row, row.Fingerprint)
			return err
		}},
		{"corrupt hash", Unavailable, func(t *testing.T) error {
			op, row := storedTestOutcome(t)
			row.ResultHash = "private corrupt hash"
			_, err := op.restore(t.Context(), row, row.Fingerprint)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.run(t)
			got := callback.Isolated("consumer", func() error { return source })
			if got == nil || got.Error() != "consumer: "+string(test.code) || !errors.Is(got, test.code) || !errors.Is(got, source) {
				t.Fatal("source diagnostic or identity was lost")
			}
		})
	}
}
