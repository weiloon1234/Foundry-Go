//go:build darwin || linux

package filelock

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestAcquireWaitsAndHonorsCancellation(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	release, err := Acquire(t.Context(), root, "lock")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, root, "lock"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("held lock was acquired", err)
	}
	if _, err := AcquireShared(ctx, root, "lock"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shared lock ignored an exclusive holder", err)
	}
	acquired := make(chan func(), 1)
	go func() {
		next, err := Acquire(t.Context(), root, "lock")
		if err != nil {
			t.Error(err)
		}
		acquired <- next
	}()
	time.Sleep(10 * time.Millisecond)
	release()
	select {
	case next := <-acquired:
		next()
	case <-time.After(3 * time.Second):
		t.Fatal("waiter was not woken by release")
	}
	// An abandoned waiter must not keep the lock once it is granted.
	first, err := AcquireShared(t.Context(), root, "lock")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AcquireShared(t.Context(), root, "lock")
	if err != nil {
		t.Fatal("shared holders excluded each other", err)
	}
	first()
	second()
	probeCtx, probeCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer probeCancel()
	probe, err := Acquire(probeCtx, root, "lock")
	if err != nil {
		t.Fatal("abandoned waiter retained the lock", err)
	}
	probe()
}

func TestConcurrentFirstAcquisitionsOfANewLockFileSucceed(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("locks", 0700); err != nil {
		t.Fatal(err)
	}
	// os.Root can report a lost O_CREATE race as not-exist; every waiter must
	// still acquire the shared lock file.
	for round := range 200 {
		name := "locks/" + strconv.Itoa(round)
		failures := make(chan error, 4)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				release, err := AcquireShared(t.Context(), root, name)
				if err != nil {
					failures <- err
					return
				}
				release()
			}()
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			t.Fatal("concurrent lock creation failed", err)
		}
	}
}

// Canceled waiters behind a paused holder retain no goroutine, thread or
// descriptor: many abandoned acquisitions leave the process as it was.
func TestCanceledWaitersLeaveNothingBehind(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	release, err := Acquire(t.Context(), root, "held")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	before := runtime.NumGoroutine()
	for range 64 {
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		if _, err := Acquire(ctx, root, "held"); !errors.Is(err, context.DeadlineExceeded) {
			cancel()
			t.Fatal("held lock was acquired", err)
		}
		cancel()
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatal("abandoned waiters kept goroutines", before, after)
	}
	// The holder is unaffected and a later waiter still gets the lock.
	release()
	next, err := Acquire(t.Context(), root, "held")
	if err != nil {
		t.Fatal(err)
	}
	next()
}
