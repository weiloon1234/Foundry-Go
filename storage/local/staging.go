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

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
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

const stagingDirectory = ".foundry-staging"
const stagingLockName = ".foundry-admission"

// Recovery runs once before readiness, independently of the listing ceiling.
// Old abandoned files move into the same accounting directory as new writes.
func (b *Backend) startStaging(ctx context.Context) (err error) {
	if err = b.root.Mkdir(stagingDirectory, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := b.root.Lstat(stagingDirectory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fs.ErrInvalid
	}
	b.staging, err = b.root.OpenRoot(stagingDirectory)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = safeerror.Join("staging startup failed", err, b.staging.Close())
			b.staging = nil
		}
	}()
	unlock, err := filelock.Acquire(ctx, b.staging, stagingLockName)
	if err != nil {
		return err
	}
	defer unlock()
	err = b.walkEntries(ctx, 0, func(parent *os.Root, directory, name string) error {
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
			if b.config.Staging.MaxActive == 0 {
				return nil
			}
			return safeerror.NewFault(fault.Conflict, "legacy staging writers must drain before enabling staging admission")
		}
		info, err := file.Stat()
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
		if !info.Mode().IsRegular() || !os.SameFile(info, current) {
			return fs.ErrInvalid
		}
		if _, err := b.staging.Lstat(name); !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return err
			}
			return fs.ErrExist
		}
		if err := b.root.Rename(directory+"/"+name, stagingDirectory+"/"+name); err != nil {
			return err
		}
		if b.config.Sync {
			return safeerror.Join("staging recovery sync failed", syncDirectory(b.staging), syncDirectory(parent))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if b.config.Sync {
		return safeerror.Join("staging startup sync failed", syncDirectory(b.staging), syncDirectory(b.root))
	}
	return nil
}

// The caller holds the staging directory's admission lock. Published objects and
// the listing/legacy-prune MaxScan budget never participate in this traversal.
func (b *Backend) walkStaging(ctx context.Context, visit func(*os.Root, string) error) error {
	return readDirectory(ctx, b.staging, func(name string) error {
		if !stagingName(name) {
			return nil
		}
		return visit(b.staging, name)
	})
}

func (b *Backend) stagingUsage(ctx context.Context) (bytes int64, active int, err error) {
	err = b.walkStaging(ctx, func(parent *os.Root, name string) error {
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

// Admission is one short cross-process critical section: inspect staging only,
// reject exhaustion immediately, otherwise create and lock one reservation.
// No input or publication callback runs under the directory lock.
func (b *Backend) createStaging(ctx context.Context, options storage.PutOptions, op storage.Operation) (*os.File, string, []byte, error) {
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
	unlock, err := filelock.Acquire(ctx, b.staging, stagingLockName)
	if err != nil {
		return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	defer unlock()
	if reserved > 0 {
		bytes, active, err := b.stagingUsage(ctx)
		if err != nil {
			return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
		}
		if reserved > b.config.Staging.MaxBytes-bytes || active >= b.config.Staging.MaxActive {
			return nil, "", nil, stagingOverload(op, nil)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	tag := make([]byte, 16)
	if _, err := rand.Read(tag); err != nil {
		return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	name := tempPrefix + hex.EncodeToString(tag)
	if reserved >= 0 {
		name += "-" + strconv.FormatInt(reserved, 10)
	}
	file, err := b.staging.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	locked, err := tryLock(file)
	if err != nil || !locked {
		cleanupErr := withdrawStaging(b.staging, stagingDirectory, name, file, op)
		return nil, "", nil, failure(storage.Unavailable, op, storage.Unchanged, safeerror.Join("staging lock failed", err, cleanupErr))
	}
	return file, name, tag, nil
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
