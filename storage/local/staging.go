package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// StagingLimits bounds large-write capacity across all processes sharing a managed
// root. Every writer of a root must use the same limits. Zero
// disables this additional limit. Known sizes at most SmallObjectBytes bypass
// the large pool; unknown sizes reserve MaxObjectBytes. MaxBytes bounds payload
// reservations, including failed cleanup. Provision record/filesystem overhead
// and retained published data separately, and reserve small-disk capacity.
type StagingLimits struct {
	MaxActive        int
	MaxBytes         int64
	SmallObjectBytes int64
}

func (s StagingLimits) validate(maximum int64) error {
	if s == (StagingLimits{}) {
		return nil
	}
	if s.MaxActive < 1 || s.MaxActive > 4096 || s.MaxBytes < maximum || s.SmallObjectBytes < 0 || s.SmallObjectBytes >= maximum {
		return failure(storage.Invalid, storage.OpenOperation, storage.Unchanged, nil)
	}
	return nil
}

// Reservation bytes are encoded in the staging entry itself. The file's writer
// lock protects the entry from Prune from admission through publication/cleanup.
// No process-local counter participates in the managed root's capacity decision.
func (b *Backend) startStaging(ctx context.Context) error {
	if b.config.Staging.MaxActive == 0 {
		return nil
	}
	_, _, err := b.stagingUsage(ctx)
	return err
}

func (b *Backend) stagingUsage(ctx context.Context) (bytes int64, active int, err error) {
	err = b.walkEntries(ctx, 0, func(parent *os.Root, _ string, name string) error {
		reserved, recognized := stagingReservation(name)
		if !recognized {
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
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fs.ErrInvalid
		}
		locked, err := tryLock(file)
		if err != nil {
			return err
		}
		current, err := parent.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !os.SameFile(info, current) {
			return fs.ErrInvalid
		}
		charge := reserved
		if reserved < 0 {
			// Legacy entries have no declared reservation. An active legacy writer
			// excludes new large work until it drains; abandoned bytes remain charged.
			charge = info.Size()
			if !locked {
				charge = max(charge, b.config.Staging.MaxBytes)
			}
		} else if reserved == 0 && locked {
			charge = info.Size()
		}
		if charge > math.MaxInt64-bytes {
			return failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
		}
		bytes += charge
		if !locked && reserved != 0 {
			active++
		}
		return nil
	})
	return bytes, active, err
}

// createStaging publishes a complete reservation before checking the root. All
// contenders include their own entry and every foreign reservation in that
// check. A writer that starts later must see earlier writers' still-live entries;
// an oversubscribed contender withdraws before retrying and never reads its input.
func (b *Backend) createStaging(ctx context.Context, parent *os.Root, directory string, options storage.PutOptions, op storage.Operation) (*os.File, string, []byte, error) {
	reserved := int64(-1)
	if b.config.Staging.MaxActive != 0 {
		reserved = b.config.MaxObjectBytes
		if size, known := options.Size.Get(); known {
			reserved = size
		}
		if reserved <= b.config.Staging.SmallObjectBytes {
			reserved = 0
		}
	}
	wait, cancel := context.WithTimeout(ctx, admission.DefaultWait)
	defer cancel()
	for {
		if err := wait.Err(); err != nil {
			return nil, "", nil, stagingOverload(op, err)
		}
		tag := make([]byte, 16)
		if _, err := rand.Read(tag); err != nil {
			return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
		}
		name := tempPrefix + hex.EncodeToString(tag)
		if reserved >= 0 {
			name += "-" + strconv.FormatInt(reserved, 10)
		}
		file, err := parent.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
		}
		locked, err := tryLock(file)
		if err != nil || !locked {
			// A concurrent pruning/inspection owner may have reached this new entry
			// before its creator acquired the lock. It has no payload yet.
			cleanupErr := withdrawStaging(parent, directory, name, file, op)
			if err != nil || cleanupErr != nil {
				return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, safeerror.Join("staging lock failed", err, cleanupErr))
			}
		} else {
			if reserved > 0 && b.beforeStagingVerify != nil {
				b.beforeStagingVerify()
			}
			bytes, active := int64(0), 0
			if reserved > 0 {
				bytes, active, err = b.stagingUsage(wait)
			}
			// A pruning owner that won the pre-lock race may already have removed the
			// name. Verify our locked descriptor still names our reservation before I/O.
			info, statErr := file.Stat()
			current, pathErr := parent.Lstat(name)
			err = safeerror.Join("staging verification failed", err, statErr, pathErr)
			if err == nil && !os.SameFile(info, current) {
				err = fs.ErrInvalid
			}
			if err == nil && (reserved <= 0 || bytes <= b.config.Staging.MaxBytes && active <= b.config.Staging.MaxActive) {
				return file, name, tag, nil
			}
			cleanupErr := withdrawStaging(parent, directory, name, file, op)
			if cleanupErr != nil {
				return nil, "", nil, cleanupErr
			}
			if err != nil {
				if wait.Err() != nil {
					return nil, "", nil, stagingOverload(op, wait.Err())
				}
				return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
			}
		}
		// Per-attempt entropy keeps simultaneous contenders from retrying in lockstep.
		timer := time.NewTimer(time.Duration(1+int(tag[0])%16) * time.Millisecond)
		select {
		case <-wait.Done():
			timer.Stop()
			return nil, "", nil, stagingOverload(op, wait.Err())
		case <-timer.C:
		}
	}
}

func withdrawStaging(parent *os.Root, directory, name string, file *os.File, op storage.Operation) error {
	// Remove while holding the writer lock. A failed removal leaves a recognizable
	// reserved entry for Prune; closing its descriptor releases only the live slot.
	removeErr := parent.Remove(name)
	if errors.Is(removeErr, fs.ErrNotExist) {
		removeErr = nil
	}
	err := safeerror.Join("staging withdrawal failed", removeErr, file.Close())
	if err == nil {
		return nil
	}
	failure := storage.Failure(storage.Unavailable, op, storage.Unchanged, err)
	if removeErr != nil {
		return failure.WithCleanup(storage.NewCleanupID(directory + "/" + name))
	}
	return failure
}

func stagingOverload(op storage.Operation, err error) error {
	return failure(storage.Unavailable, op, storage.Unchanged, safeerror.WrapFault(fault.Overloaded, "local staging capacity is exhausted", err))
}

// -1 denotes a legacy entry, zero a new small write, and a positive value the
// large write's complete payload reservation. Names are immutable until rename.
func stagingReservation(name string) (int64, bool) {
	suffix, ok := strings.CutPrefix(name, tempPrefix)
	if !ok {
		return 0, false
	}
	id, amount, encoded := strings.Cut(suffix, "-")
	if len(id) != 32 {
		return 0, false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return 0, false
	}
	if !encoded {
		return -1, true
	}
	reserved, err := strconv.ParseInt(amount, 10, 64)
	return reserved, err == nil && reserved >= 0 && reserved <= 1<<50 && strconv.FormatInt(reserved, 10) == amount
}

func stagingName(name string) bool { _, ok := stagingReservation(name); return ok }
