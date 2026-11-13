package upload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPoolRetainsFailedCleanupAndRecoversExactFiles(t *testing.T) {
	directory := t.TempDir()
	pool, err := NewPool(PoolConfig{Directory: directory, MaxActive: 2, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Claim(); err != nil {
		t.Fatal(err)
	}
	lease, err := pool.Reserve(t.Context(), 128)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := New(t.Context(), Config{TempDirectory: directory, MaxBytes: 128, MaxFileBytes: 128, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Attach(batch); err != nil {
		t.Fatal(err)
	}
	file, err := batch.Capture(t.Context(), strings.NewReader("owned data"), "file", "")
	if err != nil {
		t.Fatal(err)
	}
	// Force an exact-file unlink failure without relying on root-sensitive chmod.
	original := filepath.Join(batch.dir, file.key)
	moved := original + "-test-owned"
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(original, "test-owned")
	if err := os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if active, reserved := pool.Usage(); active != 0 || reserved != 128 {
		t.Fatalf("cleanup failure lost charge: %d/%d", active, reserved)
	}
	if _, err := pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Overloaded) {
		t.Fatalf("retained occupancy admitted: %v", err)
	}
	if err := pool.RetryCleanup(); err == nil {
		t.Fatal("cleanup unexpectedly succeeded")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatal("unowned filename was removed")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, original); err != nil {
		t.Fatal(err)
	}
	if err := pool.RetryCleanup(); err != nil {
		t.Fatal(err)
	}
	next, err := pool.Reserve(t.Context(), 128)
	if err != nil {
		t.Fatal(err)
	}
	next.Release()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewPool(PoolConfig{Directory: directory, MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Claim(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	} // Must not remove the new owner's marker.
	contender, err := NewPool(PoolConfig{Directory: directory, MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := contender.Claim(); !errors.Is(err, fault.Conflict) {
		t.Fatal("old close removed new ownership", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPoolCancellationRetainsActualCaptureOwnership(t *testing.T) {
	pool, err := NewPool(PoolConfig{Directory: t.TempDir(), MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := pool.Claim(); err != nil {
		t.Fatal(err)
	}
	lease, err := pool.Reserve(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if active, reserved := pool.Usage(); active != 1 || reserved != 128 {
		t.Fatal("cancellation released capacity")
	}
	if err := pool.Close(); !errors.Is(err, fault.Conflict) {
		t.Fatal("close discarded active ownership")
	}
	lease.Release()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

type poolBlockedReader struct{ entered, exit chan struct{} }

func (r *poolBlockedReader) Read([]byte) (int, error) {
	close(r.entered)
	<-r.exit
	return 0, context.Canceled
}

func TestPoolWaitsForActualCaptureExit(t *testing.T) {
	pool, err := NewPool(PoolConfig{Directory: t.TempDir(), MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := pool.Claim(); err != nil {
		t.Fatal(err)
	}
	reservation, err := pool.Reserve(ctx, 128)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := New(ctx, Config{TempDirectory: pool.Directory(), MaxBytes: 128, MaxFileBytes: 128, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Attach(batch); err != nil {
		t.Fatal(err)
	}
	source := &poolBlockedReader{entered: make(chan struct{}), exit: make(chan struct{})}
	captured := make(chan struct{})
	go func() { defer close(captured); _, _ = batch.Capture(ctx, source, "file", "") }()
	<-source.entered
	cancel()
	released := make(chan struct{})
	go func() { defer close(released); reservation.Release() }()
	select {
	case <-released:
		t.Fatal("released a blocked capture")
	case <-time.After(20 * time.Millisecond):
	}
	if active, reserved := pool.Usage(); active != 1 || reserved != 128 {
		t.Fatal("blocked capture lost capacity")
	}
	if _, err := pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Overloaded) {
		t.Fatal("blocked capture admitted more work")
	}
	close(source.exit)
	<-captured
	<-released
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPoolCleanupRefusesReplacedDirectory(t *testing.T) {
	pool, err := NewPool(PoolConfig{Directory: t.TempDir(), MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Claim(); err != nil {
		t.Fatal(err)
	}
	reservation, err := pool.Reserve(t.Context(), 128)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := New(t.Context(), Config{TempDirectory: pool.Directory(), MaxBytes: 128, MaxFileBytes: 128, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Attach(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Capture(t.Context(), strings.NewReader("owned"), "file", ""); err != nil {
		t.Fatal(err)
	}
	directory := batch.dir
	sentinel := filepath.Join(directory, "test-owned-sentinel")
	if err := os.WriteFile(sentinel, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	moved := directory + "-test-owned"
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := pool.RetryCleanup(); err == nil {
		t.Fatal("cleanup accepted a different directory")
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatal("cleanup removed a replacement directory")
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}
	if err := pool.RetryCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}
