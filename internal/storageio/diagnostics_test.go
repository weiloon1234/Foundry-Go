package storageio

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
)

type privateTransferError struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateTransferError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private transfer")
}
func (e *privateTransferError) Format(fmt.State, rune) { _ = e.Error() }

type errorReader struct{ failure error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.failure }

func TestTransferBoundaryDiagnostics(t *testing.T) {
	for _, exit := range []bool{false, true} {
		cause := &privateTransferError{exit: exit}
		for _, test := range []struct {
			name    string
			source  io.Reader
			message string
			code    storage.Code
		}{
			{"limit", strings.NewReader("long"), "storage byte transfer: storage_limit_exceeded: put", storage.LimitExceeded},
			{"integrity", badReader(-1), "storage byte transfer: storage_integrity_failed: put", storage.IntegrityFailed},
			{"application", errorReader{cause}, "storage byte transfer failed", ""},
		} {
			t.Run(fmt.Sprintf("%s/%t", test.name, exit), func(t *testing.T) {
				_, _, err := Copy(t.Context(), io.Discard, test.source, 1)
				if err == nil || err.Error() != test.message {
					t.Fatal("transfer diagnostic changed", err)
				}
				if test.code != "" {
					var retained *storage.Error
					if !errors.Is(err, test.code) || !errors.As(err, &retained) || retained.Outcome() != storage.Unchanged {
						t.Fatal("transfer classification/identity lost")
					}
				} else {
					var retained *privateTransferError
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("private identity lost")
					}
				}
				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				if cause.calls.Load() != 0 {
					t.Fatal("transfer formatted application cause")
				}
			})
		}
	}
}
