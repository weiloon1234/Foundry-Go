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
			if _, err := backend.Put(stagingBusyContext(t), stagingKey(t, "excess"), reject, size); !errors.Is(err, fault.Overloaded) {
				t.Fatal("large write admitted", err)
			}
			if reads.Load() != 0 {
				t.Fatal("overloaded write consumed its source")
			}
			if _, err := backend.Copy(stagingBusyContext(t), source, stagingKey(t, "copy-target"), storage.CopyOptions{}, 8); !errors.Is(err, fault.Overloaded) {
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
			files, _ := filepath.Glob(filepath.Join(config.Root, stagingDirectory, tempPrefix+"*"))
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
	path := filepath.Join(config.Root, stagingDirectory)
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
	if _, err := backend.Put(stagingBusyContext(t), stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
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
	if _, err := backend.Put(stagingBusyContext(t), stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
		t.Fatal("restart forgot interrupted staging", err)
	}
	if n, err := backend.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := backend.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestStagingRecoveryIgnoresPublishedListingCeiling(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		name := "healthy"
		if orphan {
			name = "with-orphan"
		}
		t.Run(name, func(t *testing.T) {
			config := DefaultConfig(t.TempDir())
			config.MaxScan, config.MaxObjectBytes = 1, 8
			config.Staging = StagingLimits{MaxActive: 1, MaxBytes: 8, SmallObjectBytes: 2}
			backend, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"one", "two", "three"} {
				if _, err := backend.Put(t.Context(), stagingKey(t, name), strings.NewReader("ok"), storage.PutOptions{Size: value.Set[int64](2)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			if orphan {
				directory := filepath.Join(config.Root, "objects", "ff")
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, tempPrefix+strings.Repeat("b", 32)), []byte("orphaned"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			backend, err = Open(t.Context(), config)
			if err != nil {
				t.Fatal("published objects exceeded listing ceiling during recovery", err)
			}
			defer backend.Close()
			ctx := t.Context()
			if orphan {
				ctx = stagingBusyContext(t)
			}
			_, err = backend.Put(ctx, stagingKey(t, "after-restart"), strings.NewReader("next"), storage.PutOptions{})
			if orphan {
				if !errors.Is(err, fault.Overloaded) {
					t.Fatal("recovery stopped before accounting for the final shard", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStagingRecoveryReconcilesOtherOwners(t *testing.T) {
	for _, outcome := range []string{"publish", "abort", "prune", "replace"} {
		t.Run(outcome, func(t *testing.T) {
			config := DefaultConfig(t.TempDir())
			config.MaxObjectBytes = 8
			config.Staging = StagingLimits{MaxActive: 1, MaxBytes: 8, SmallObjectBytes: 2}
			writer, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			var release func()
			if outcome == "publish" || outcome == "abort" {
				entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
				var once sync.Once
				release = func() { once.Do(func() { close(resume) }) }
				joined := false
				defer func() {
					release()
					if !joined {
						<-done
					}
				}()
				key := stagingKey(t, "live-writer")
				go func() {
					tail := stagingReaderFunc(func([]byte) (int, error) {
						close(entered)
						<-resume
						if outcome == "abort" {
							return 0, io.ErrUnexpectedEOF
						}
						return 0, io.EOF
					})
					_, err := writer.Put(context.Background(), key, io.MultiReader(strings.NewReader("payload"), tail), storage.PutOptions{})
					done <- err
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("writer did not enter")
				}
				peer, err := Open(t.Context(), config)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Close()
				// Both backends must see the same root reservation. Restart cannot
				// create extra capacity, and Prune must respect the writer lock.
				if _, err := writer.Put(stagingBusyContext(t), stagingKey(t, "writer-full"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
					t.Fatal("live reservation released early", err)
				}
				if _, err := peer.Put(stagingBusyContext(t), stagingKey(t, "peer-live"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
					t.Fatal("foreign live writer was omitted from the root budget", err)
				}
				if n, err := peer.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 0 {
					t.Fatal("live writer pruned", n, err)
				}
				release()
				err = <-done
				joined = true
				if (err != nil) != (outcome == "abort") {
					t.Fatal("unexpected writer outcome", err)
				}
				if _, err := peer.Put(t.Context(), stagingKey(t, "peer-after"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
					t.Fatal("foreign writer left a stale charge", err)
				}
				return
			}
			directory, _ := address(stagingKey(t, "orphan"))
			directory = filepath.Join(config.Root, directory)
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, tempPrefix+strings.Repeat("a", 32))
			if err := os.WriteFile(path, []byte("orphaned"), 0600); err != nil {
				t.Fatal(err)
			}
			peer, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("legacy staging was not migrated", err)
			}
			path = filepath.Join(config.Root, stagingDirectory, filepath.Base(path))
			if _, err := peer.Put(stagingBusyContext(t), stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
				t.Fatal("recovery did not account before admission", err)
			}
			if outcome == "prune" {
				if n, err := writer.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 1 {
					t.Fatal(n, err)
				}
			} else {
				replacement := path + "-replacement"
				if err := os.WriteFile(replacement, []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
				// The new inode still consumes three bytes, but must not inherit the old
				// inode's eight-byte charge or disappear merely because its name is reused.
				if _, err := peer.Put(stagingBusyContext(t), stagingKey(t, "still-blocked"), strings.NewReader("next"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
					t.Fatal("replacement was not charged", err)
				}
			}
			if _, err := peer.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), storage.PutOptions{Size: value.Set[int64](4)}); err != nil {
				t.Fatal("foreign removal/replacement retained stale bytes", err)
			}
		})
	}
}

func stagingBusyContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}
