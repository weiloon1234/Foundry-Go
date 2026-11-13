package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// Prune removes only recognized, old staging files whose live-writer lock can
// be acquired. A slow upload is never removed merely because it is old. A crash
// releases its file lock. Published objects and marker/lock files are untouched.
// limit bounds deletion; Config.MaxScan bounds the legacy object-shard scan.
// The dedicated staging directory is scanned separately in bounded batches.
func (b *Backend) Prune(ctx context.Context, olderThan time.Time, limit int) (uint64, error) {
	if err := b.ready(ctx, storage.DeleteOperation); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPrune || olderThan.IsZero() || olderThan.After(b.config.Clock.Now()) {
		return 0, failure(storage.Invalid, storage.DeleteOperation, storage.Unchanged, nil)
	}
	var removed uint64
	finished := safeerror.New("staging cleanup batch complete")
	visit := func(parent *os.Root, name string) error {
		if !stagingName(name) {
			return nil
		}
		file, err := parent.OpenFile(name, os.O_RDWR, 0)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer file.Close()
		locked, err := tryLock(file)
		if err != nil {
			return err
		}
		if !locked {
			return nil
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(olderThan) {
			return nil
		}
		current, err := parent.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !os.SameFile(info, current) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = parent.Remove(name); err != nil {
			return err
		}
		removed++
		if b.config.Sync {
			if err = syncDirectory(parent); err != nil {
				return err
			}
		}
		if removed == uint64(limit) {
			return finished
		}
		return nil
	}
	// Serialize staging cleanup with entry creation, never with an admitted writer.
	unlock, err := filelock.Acquire(ctx, b.staging, stagingLockName)
	if err != nil {
		return 0, failure(storage.Unavailable, storage.DeleteOperation, storage.NotApplicable, err)
	}
	err = b.walkStaging(ctx, visit)
	unlock()
	if err == nil {
		// Older runs may have left staging in object shards. That legacy pass keeps
		// its existing MaxScan bound; the dedicated directory never consumes it.
		err = b.walkEntries(ctx, b.config.MaxScan, func(parent *os.Root, _ string, name string) error { return visit(parent, name) })
	}

	if errors.Is(err, finished) {
		err = nil
	}
	if err != nil {
		return removed, failure(storage.Unavailable, storage.DeleteOperation, storage.NotApplicable, err)
	}
	return removed, nil
}
