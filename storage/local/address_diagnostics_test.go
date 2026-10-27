package local_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

type diagnosticLocator struct {
	storage.Backend
	cause error
}

func (b diagnosticLocator) Locate(storage.ObjectKey) (storage.ObjectAddress, error) {
	return storage.ObjectAddress{}, b.cause
}

type hostileLocatorError struct{ calls atomic.Int32 }

func (e *hostileLocatorError) Error() string {
	e.calls.Add(1)
	panic("private locator diagnostic")
}
func (e *hostileLocatorError) Format(fmt.State, rune) { _ = e.Error() }

func TestLocalAddressDiagnosticsThroughMove(t *testing.T) {
	for _, name := range []string{"nil backend", "unstarted backend", "application code", "hostile locator"} {
		t.Run(name, func(t *testing.T) {
			_, backend := openLocal(t, t.TempDir())
			destination, _ := openLocal(t, t.TempDir())
			object := key(t, "diagnostic/object")
			private := &hostileLocatorError{}
			var source error
			code := storage.Unavailable
			want := "consumer: storage_unavailable: move"
			switch name {
			case "nil backend":
				_, source = (*local.Backend)(nil).Locate(object)
				code = storage.Invalid
			case "unstarted backend":
				_, source = (&local.Backend{}).Locate(object)
				code = storage.Closed
			case "application code":
				source, code = storage.Invalid, storage.Invalid
			case "hostile locator":
				source = private
			}
			if name == "nil backend" || name == "unstarted backend" {
				if source.Error() != string(code) {
					t.Fatal("source changed its exact diagnostic")
				}
				want = "consumer: storage object address: " + string(code)
			}
			disk, err := storage.NewDisk("diagnostic", diagnosticLocator{Backend: backend, cause: source}, storage.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer disk.Close(context.Background())
			var result storage.MoveResult
			var original error
			got := callback.Isolated("consumer", func() error {
				result, original = disk.MoveTo(t.Context(), object, destination, object, storage.CopyOptions{})
				return original
			})
			var detail *storage.Error
			if got == nil || got.Error() != want || !errors.Is(got, source) || !errors.Is(got, original) || !errors.Is(got, code) || !errors.As(got, &detail) {
				t.Fatal("address diagnostic, classification or identity was lost")
			}
			if detail.Code() != storage.Unavailable || detail.Operation() != storage.MoveOperation || detail.Outcome() != storage.Unknown || result.SourceOutcome != storage.Unchanged || result.DestinationOutcome != storage.Unchanged {
				t.Fatal("address failure changed conservative storage outcomes")
			}
			if private.calls.Load() != 0 || disk.Stats().Active != 0 {
				t.Fatal("locator formatting ran or admission leaked")
			}
		})
	}
}
