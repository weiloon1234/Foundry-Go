package filelock

import (
	"context"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type mode uint8

const (
	exclusive mode = iota
	shared
)

// Acquire owns a distinct descriptor, serializing goroutines and processes.
// The lock file must never be removed while the root can still be in use.
func Acquire(ctx context.Context, root *os.Root, name string) (func(), error) {
	return acquire(ctx, root, name, exclusive)
}

// AcquireShared takes a shared lock that excludes only exclusive holders.
func AcquireShared(ctx context.Context, root *os.Root, name string) (func(), error) {
	return acquire(ctx, root, name, shared)
}

// Contended acquisition polls with non-blocking flock and a jittered backoff
// (firstPoll doubling to maxPoll). A blocking flock would pin one goroutine,
// one OS thread and one descriptor per waiter until the holder releases, and a
// canceled waiter could not reclaim them while a holder is paused.
const (
	firstPoll = time.Millisecond
	maxPoll   = 25 * time.Millisecond
)

// acquire takes the lock when it is free; otherwise it retries non-blocking
// attempts until it succeeds or ctx ends. A canceled wait returns at once and
// closes its descriptor, so an abandoned waiter holds no thread or file.
func acquire(ctx context.Context, root *os.Root, name string, m mode) (func(), error) {
	if ctx == nil || root == nil {
		return nil, fault.New(fault.Invalid, "file lock requires a context and root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := open(root, name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		_ = file.Close()
		return nil, fault.New(fault.Invalid, "invalid file lock")
	}
	release := func() { _ = file.Close() }
	delay := firstPoll
	var timer *time.Timer
	for {
		locked, err := try(file, m)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			if timer != nil {
				timer.Stop()
			}
			return release, nil
		}
		wait := delay/2 + time.Duration(rand.Int64N(int64(delay-delay/2)+1))
		delay = min(delay*2, maxPoll)
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			timer.Reset(wait)
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// createAttempts bounds retries of a lock-file open that lost a creation race.
const createAttempts = 8

// open opens or creates the lock file. os.Root can report a concurrent
// O_CREATE of the same new name as not-exist; the file then exists, so a
// bounded retry succeeds. A genuinely missing parent still fails.
func open(root *os.Root, name string) (*os.File, error) {
	var file *os.File
	var err error
	for range createAttempts {
		file, err = root.OpenFile(name, os.O_RDWR|os.O_CREATE, 0600)
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	return file, err
}
