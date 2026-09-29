package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/weiloon1234/Foundry-Go/internal/storageio"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (b *Backend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	if err := b.ready(ctx, storage.PutOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := key.Validate(); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := options.Validate(b.config.MaxObjectBytes); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.Capabilities().ValidatePut(options); err != nil {
		return storage.ObjectInfo{}, err
	}
	if source == nil {
		return storage.ObjectInfo{}, failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	contentType := options.ContentType
	if contentType == "" {
		contentType = storage.Binary
	}
	return b.publish(ctx, key, contentType, options, storage.PutOperation, func(file *os.File) (int64, storage.SHA256, error) {
		maximum := b.config.MaxObjectBytes
		if size, present := options.Size.Get(); present {
			maximum = size
		}
		return storageio.Copy(ctx, file, source, maximum)
	})
}

// publish writes one staging file (header, payload from fill) in the key's
// shard, syncs it, then checks the write condition and renames it into place
// under the key's shard lock. An unconditional publication releases the lock
// before the staging descriptor closes and before the directory sync; a
// conditional one holds it through that sync. Success is reported only after
// the sync.
func (b *Backend) publish(ctx context.Context, key storage.ObjectKey, contentType storage.MediaType, options storage.PutOptions, op storage.Operation, fill func(*os.File) (int64, storage.SHA256, error)) (result storage.ObjectInfo, err error) {
	directory, name := address(key)
	shard, _ := hex.DecodeString(name[:2])
	if err = b.ensureShard(directory, shard[0]); err != nil {
		return result, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	parent, err := b.root.OpenRoot(directory)
	if err != nil {
		return result, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	defer parent.Close()
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return result, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	temporary := tempPrefix + hex.EncodeToString(random)
	file, err := parent.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, failure(storage.Unavailable, op, storage.Unchanged, err)
	}
	state := storage.Unchanged
	closed := false
	published := false
	var unlock func()
	defer func() {
		if unlock != nil {
			unlock()
		}
		var closeErr, removeErr error
		if !closed {
			closeErr = file.Close()
		}
		if !published {
			removeErr = parent.Remove(temporary)
			if errors.Is(removeErr, fs.ErrNotExist) {
				removeErr = nil
			}
		}
		if err != nil || closeErr != nil || removeErr != nil {
			result = storage.ObjectInfo{}
			err = failure(storage.Unavailable, op, state, errors.Join(err, closeErr, removeErr))
			if removeErr != nil {
				err = err.(*storage.Error).WithCleanup(storage.NewCleanupID(directory + "/" + temporary))
			}
		}
	}()
	// A process crash releases this lock. Cleanup skips every live staging file,
	// regardless of its age; it never guesses that a slow source has finished.
	if locked, e := tryLock(file); e != nil || !locked {
		return result, failure(storage.Unavailable, op, storage.Unchanged, e)
	}
	modified, err := temporal.NewDateTime(b.config.Clock.Now())
	if err != nil {
		return result, err
	}
	info := storage.ObjectInfo{Key: key, ContentType: contentType, Modified: modified, ETag: storage.ETag("\"" + hex.EncodeToString(random) + "\"")}
	header := encodeHeader(info, random)
	if _, err = file.Write(header); err != nil {
		return result, err
	}
	size, digest, err := fill(file)
	if err != nil {
		return result, err
	}
	if expected, present := options.Size.Get(); present && expected != size {
		return result, failure(storage.IntegrityFailed, op, storage.Unchanged, nil)
	}
	if expected, present := options.Checksum.Get(); present && expected != digest {
		return result, failure(storage.IntegrityFailed, op, storage.Unchanged, nil)
	}
	info.Size = size
	info.Checksum = value.Set(digest)
	info.Modified, err = temporal.NewDateTime(b.config.Clock.Now())
	if err != nil {
		return result, err
	}
	if err = info.Validate(); err != nil {
		return result, err
	}
	header = encodeHeader(info, random)
	if _, err = file.WriteAt(header, 0); err != nil {
		return result, err
	}
	if b.config.Sync {
		if err = file.Sync(); err != nil {
			return result, err
		}
	}
	unlock, err = b.lockKey(ctx, key)
	if err != nil {
		return result, err
	}
	if err = b.checkWrite(key, options.Condition); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// Keep the staging descriptor/lock until rename so cleanup cannot remove a
	// completed but not-yet-published file. Readers see only immutable publications.
	if err = parent.Rename(temporary, name); err != nil {
		return result, err
	}
	published = true
	state = storage.Applied
	// An unconditional publication releases the shard before its durability
	// sync. A conditional one keeps it until the rename is durable, so no other
	// conditional writer acts on a publication that could still be lost.
	if !options.Condition.RequiresAbsence() && options.Condition.Match() == "" {
		unlock()
		unlock = nil
	}
	err = file.Close()
	closed = true
	if err != nil {
		return result, err
	}
	if b.beforeSync != nil {
		b.beforeSync(key)
	}
	if b.config.Sync {
		if err = syncDirectory(parent); err != nil {
			return result, err
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	return info, nil
}
func (b *Backend) checkWrite(key storage.ObjectKey, condition storage.WriteCondition) error {
	if !condition.RequiresAbsence() && condition.Match() == "" {
		return nil
	}
	file, info, _, err := b.openRecord(key)
	if err != nil {
		if errors.Is(err, storage.NotFound) {
			if condition.RequiresAbsence() {
				return nil
			}
			return failure(storage.PreconditionFailed, storage.PutOperation, storage.Unchanged, nil)
		}
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if condition.RequiresAbsence() || info.ETag != condition.Match() {
		return failure(storage.PreconditionFailed, storage.PutOperation, storage.Unchanged, nil)
	}
	return nil
}

// Copy implements storage.ServerCopier within one managed store. It writes a
// new record (the header binds its key) and copies the payload file-to-file,
// which uses kernel copy offload where the platform provides it, never the
// disk's streaming read path. The source is pinned by its validator and the
// destination publishes with the same condition checks as Put.
func (b *Backend) Copy(ctx context.Context, source, target storage.ObjectKey, options storage.CopyOptions, maximum int64) (storage.ObjectInfo, error) {
	if err := b.ready(ctx, storage.CopyOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := source.Validate(); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := target.Validate(); err != nil {
		return storage.ObjectInfo{}, err
	}
	write := options.Destination
	if err := b.readOptions(options.Source); err != nil {
		return storage.ObjectInfo{}, err
	}
	if options.Source.Range.IsSet() {
		return storage.ObjectInfo{}, failure(storage.Invalid, storage.CopyOperation, storage.Unchanged, nil)
	}
	if err := write.Validate(b.config.MaxObjectBytes); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.Capabilities().ValidatePut(write); err != nil {
		return storage.ObjectInfo{}, err
	}
	file, info, offset, err := b.openRecord(source)
	if err != nil {
		return storage.ObjectInfo{}, failure(storage.Unavailable, storage.CopyOperation, storage.Unchanged, err)
	}
	defer file.Close()
	if options.Source.IfMatch != "" && options.Source.IfMatch != info.ETag {
		return storage.ObjectInfo{}, failure(storage.PreconditionFailed, storage.CopyOperation, storage.Unchanged, nil)
	}
	if info.Size > min(maximum, b.config.MaxObjectBytes) {
		return storage.ObjectInfo{}, failure(storage.LimitExceeded, storage.CopyOperation, storage.Unchanged, nil)
	}
	if size, supplied := write.Size.Get(); supplied && size != info.Size {
		return storage.ObjectInfo{}, failure(storage.Invalid, storage.CopyOperation, storage.Unchanged, nil)
	}
	checksum, _ := info.Checksum.Get()
	if expected, declared := write.Checksum.Get(); declared && expected != checksum {
		return storage.ObjectInfo{}, failure(storage.IntegrityFailed, storage.CopyOperation, storage.Unchanged, nil)
	}
	contentType := write.ContentType
	if contentType == "" {
		contentType = info.ContentType
	}
	write.Size, write.Checksum = value.Set(info.Size), value.Set(checksum)
	return b.publish(ctx, target, contentType, write, storage.CopyOperation, func(staging *os.File) (int64, storage.SHA256, error) {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return 0, storage.SHA256{}, err
		}
		// Bounded chunks keep cancellation observable during a large copy.
		var copied int64
		for copied < info.Size {
			if err := ctx.Err(); err != nil {
				return copied, checksum, err
			}
			n, err := io.Copy(staging, &io.LimitedReader{R: file, N: min(info.Size-copied, copyChunkBytes)})
			copied += n
			if err != nil {
				return copied, checksum, err
			}
			if n == 0 {
				return copied, checksum, failure(storage.IntegrityFailed, storage.CopyOperation, storage.Unchanged, io.ErrUnexpectedEOF)
			}
		}
		return copied, checksum, ctx.Err()
	})
}

const copyChunkBytes = 8 << 20

var _ storage.ServerCopier = (*Backend)(nil)
