//go:build darwin || linux

package local

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type stagingReaderFunc func([]byte) (int, error)

func (f stagingReaderFunc) Read(p []byte) (int, error) { return f(p) }

func stagingKey(t *testing.T, name string) storage.ObjectKey {
	t.Helper()
	key, err := storage.ParseKey(name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestStagingAdmissionPreservesSmallWritesAndActualLifetime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		slots  int
		budget int64
		known  bool
	}{
		{"slots", 1, 16, true}, {"bytes-and-unknown-length", 2, 8, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig(t.TempDir())
			config.MaxObjectBytes = 8
			config.Staging = StagingLimits{MaxActive: tc.slots, MaxBytes: tc.budget, SmallObjectBytes: 2}
			backend, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			size := storage.PutOptions{Size: value.Set[int64](4)}
			source := stagingKey(t, "copy-source")
			if _, err := backend.Put(t.Context(), source, strings.NewReader("copy"), size); err != nil {
				t.Fatal(err)
			}
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			var once sync.Once
			defer func() { once.Do(func() { close(release) }); <-done }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := storage.PutOptions{}
			if tc.known {
				options = size
			}
			go func() {
				_, err := backend.Put(ctx, stagingKey(t, "held"), stagingReaderFunc(func([]byte) (int, error) {
					close(entered)
					<-release // Deliberately ignores cancellation: admission must remain held.
					return 0, io.ErrUnexpectedEOF
				}), options)
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not enter")
			}
			cancel()
			var reads atomic.Int64
			reject := stagingReaderFunc(func([]byte) (int, error) { reads.Add(1); return 0, io.EOF })
			if _, err := backend.Put(t.Context(), stagingKey(t, "excess"), reject, size); !errors.Is(err, fault.Overloaded) {
				t.Fatal("large write admitted", err)
			}
			if reads.Load() != 0 {
				t.Fatal("overloaded write consumed its source")
			}
			if _, err := backend.Copy(t.Context(), source, stagingKey(t, "copy-target"), storage.CopyOptions{}, 8); !errors.Is(err, fault.Overloaded) {
				t.Fatal("copy bypassed staging limits", err)
			}
			if _, err := backend.Put(t.Context(), stagingKey(t, "small"), strings.NewReader("ok"), storage.PutOptions{Size: value.Set[int64](2)}); err != nil {
				t.Fatal("small capacity lost", err)
			}
			once.Do(func() { close(release) })
			select {
			case err := <-done:
				done <- err // Leave the deferred drain responsible for joining the writer.
				if err == nil {
					t.Fatal("interrupted writer succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not exit")
			}
			if _, err := backend.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), size); err != nil {
				t.Fatal("capacity did not recover", err)
			}
			files, _ := filepath.Glob(filepath.Join(config.Root, "objects", "*", tempPrefix+"*"))
			if len(files) != 0 {
				t.Fatal("staging survived cleanup", files)
			}
		})
	}
}

func TestStagingFailedCleanupRetainsBytesUntilPrune(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fault requires an unprivileged filesystem owner")
	}
	config := DefaultConfig(t.TempDir())
	config.MaxObjectBytes = 8
	config.Staging = StagingLimits{MaxActive: 2, MaxBytes: 8, SmallObjectBytes: 2}
	backend, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	key := stagingKey(t, "failed-cleanup")
	directory, _ := address(key)
	path := filepath.Join(config.Root, directory)
	defer os.Chmod(path, 0700)
	_, err = backend.Put(t.Context(), key, stagingReaderFunc(func([]byte) (int, error) {
		if err := os.Chmod(path, 0500); err != nil {
			return 0, err
		}
		return 0, io.ErrUnexpectedEOF
	}), storage.PutOptions{})
	if chmodErr := os.Chmod(path, 0700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	var detail *storage.Error
	if err == nil || !errors.As(err, &detail) || !detail.Cleanup().IsSet() {
		t.Fatal("missing cleanup failure", err)
	}
	if _, err := backend.Put(t.Context(), stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
		t.Fatal("failed cleanup released its bytes", err)
	}
	if n, err := backend.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 1 {
		t.Fatal("cleanup did not recover reservation", n, err)
	}
	if _, err := backend.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestStagingRestartAccountsForInterruptedRecords(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	config.MaxObjectBytes = 8
	config.Staging = StagingLimits{MaxActive: 1, MaxBytes: 8, SmallObjectBytes: 2}
	backend, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	directory, _ := address(stagingKey(t, "interrupted"))
	path := filepath.Join(config.Root, directory)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(path, tempPrefix+strings.Repeat("0", 32))
	if err := os.WriteFile(path, []byte("orphaned"), 0600); err != nil {
		t.Fatal(err)
	}
	backend, err = Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if _, err := backend.Put(t.Context(), stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
		t.Fatal("restart forgot interrupted staging", err)
	}
	if n, err := backend.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := backend.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
}
