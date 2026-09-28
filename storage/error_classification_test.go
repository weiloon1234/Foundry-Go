package storage_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
)

type storageClassificationError struct{ inspect func() }

func (storageClassificationError) Error() string { return "private adapter error" }
func (e storageClassificationError) As(any) bool { e.inspect(); return false }

func TestStorageErrorInspectionCannotEscapeOrReleaseOwnership(t *testing.T) {
	for _, mode := range []string{"panic", "goexit", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			failure := storageClassificationError{inspect: func() {
				close(entered)
				switch mode {
				case "panic":
					panic("private panic")
				case "goexit":
					runtime.Goexit()
				default:
					<-release
				}
			}}
			disk := diskFor(t, &backend{put: func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
				return storage.ObjectInfo{}, failure
			}}, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			key := testKey(t)
			go func() { _, err := disk.Put(ctx, key, strings.NewReader("pending"), storage.PutOptions{}); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("classification did not start")
			}
			if mode == "blocked" {
				cancel()
				wait, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
				err := disk.Close(wait)
				stop()
				if !errors.Is(err, context.DeadlineExceeded) || disk.Stats().Active != 1 {
					t.Fatal("classification abandoned storage owner")
				}
				release <- struct{}{}
			}
			select {
			case err := <-done:
				var detail *storage.Error
				if !errors.As(err, &detail) || detail.Outcome() != storage.Unknown || strings.Contains(err.Error(), "private") {
					t.Fatal("failure lost conservative/redacted outcome", err)
				}
			case <-time.After(time.Second):
				t.Fatal("error inspection stranded storage")
			}
		})
	}
}
