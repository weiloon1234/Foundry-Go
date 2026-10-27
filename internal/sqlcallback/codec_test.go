package sqlcallback

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type forgedCodecScanner struct {
	*codecScanner
	fail func()
}

func (s forgedCodecScanner) Scan(any) error { s.fail(); return nil }

type privateCodecFailure struct{ calls int }

func (e *privateCodecFailure) Error() string          { e.calls++; panic("private scanner text") }
func (e *privateCodecFailure) Format(fmt.State, rune) { e.calls++; runtime.Goexit() }

func TestNativeCodecScannerSealsWithoutLosingPublicIdentity(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		cause := &privateCodecFailure{}
		destination := int64(7)
		source := NewScanner(func(any) (int64, error) { return 99, cause }, &destination, trusted)
		public := source.Scan(int64(1))
		values := []any{source}
		native := Destinations(values)[0].(sql.Scanner).Scan(int64(1))
		if values[0] != source {
			t.Fatal("borrowed destinations mutated")
		}
		if !errors.Is(public, cause) || errors.Is(native, cause) {
			t.Fatal("public/native cause boundary changed")
		}
		restored := safeerror.Reveal("scan failed", native)
		var identity *privateCodecFailure
		if !errors.Is(restored, cause) || !errors.As(restored, &identity) || identity != cause || destination != 7 {
			t.Fatal("cause or failed destination changed")
		}
		if text := fmt.Sprintf("%v %+v %#v", native, native, native); strings.Contains(text, "private") || cause.calls != 0 {
			t.Fatal("application formatting invoked")
		}
	}
}

func TestOverriddenCodecScannerCannotInheritCapability(t *testing.T) {
	for _, exit := range []bool{false, true} {
		var destination int64
		real := NewScanner(func(v any) (int64, error) { return v.(int64), nil }, &destination, true).(*codecScanner)
		for _, embedded := range []*codecScanner{real, nil} {
			exited := false
			forged := forgedCodecScanner{codecScanner: embedded, fail: func() {
				defer func() { exited = true }()
				if exit {
					runtime.Goexit()
				}
				panic("private forged scanner")
			}}
			err := Destinations([]any{forged})[0].(sql.Scanner).Scan(int64(1))
			err = safeerror.Reveal("scan failed", err)
			if !errors.Is(err, fault.Panicked) || !exited || destination != 0 || strings.Contains(err.Error(), "private") {
				t.Fatal("forged scanner escaped containment")
			}
		}
	}
}

func TestScanBufferPreservesBorrowedSlicesAndRefreshesDestinations(t *testing.T) {
	var buffer ScanBuffer
	for i := int64(1); i <= 3; i++ {
		var result int64
		original := NewScanner(func(v any) (int64, error) { return v.(int64), nil }, &result, true)
		input := []any{original}
		if err := buffer.Prepare(input)[0].(sql.Scanner).Scan(i); err != nil || result != i || input[0] != original {
			t.Fatal("scanner scratch retained a stale destination or mutated input")
		}
	}
	buffer.Reset()
	if buffer.values != nil {
		t.Fatal("closed stream retained destinations")
	}
}
