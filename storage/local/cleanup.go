package local

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
)

// Prune removes only recognized, old staging files whose live-writer lock can
// be acquired. A slow upload is never removed merely because it is old. A crash
// releases its file lock. Published objects and marker/lock files are untouched.
// limit bounds deletion; Config.MaxScan independently bounds scanning work.
func (b *Backend) Prune(ctx context.Context, olderThan time.Time, limit int) (uint64, error) {
	if err := b.ready(ctx, storage.DeleteOperation); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPrune || olderThan.IsZero() || olderThan.After(b.config.Clock.Now()) {
		return 0, failure(storage.Invalid, storage.DeleteOperation, storage.Unchanged, nil)
	}
	var removed uint64
	finished := errors.New("staging cleanup batch complete")
	err := b.walk(ctx, func(parent *os.Root, name string) error {
		suffix, ok := strings.CutPrefix(name, tempPrefix)
		if !ok || len(suffix) != 32 {
			return nil
		}
		if _, err := hex.DecodeString(suffix); err != nil {
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
	})
	if errors.Is(err, finished) {
		err = nil
	}
	if err != nil {
		return removed, failure(storage.Unavailable, storage.DeleteOperation, storage.NotApplicable, err)
	}
	return removed, nil
}
