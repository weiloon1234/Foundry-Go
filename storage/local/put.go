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

func (b *Backend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (result storage.ObjectInfo, err error) {
	if err = b.ready(ctx, storage.PutOperation); err != nil {
		return result, err
	}
	if err = key.Validate(); err != nil {
		return result, err
	}
	if err = options.Validate(b.config.MaxObjectBytes); err != nil {
		return result, err
	}
	if source == nil {
		return result, failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	directory, name := address(key)
	if err = b.root.MkdirAll(directory, 0700); err != nil {
		return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	if b.config.Sync {
		objects, e := b.root.OpenRoot("objects")
		if e != nil {
			return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, e)
		}
		e = errors.Join(syncDirectory(objects), objects.Close(), syncDirectory(b.root))
		if e != nil {
			return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, e)
		}
	}
	parent, err := b.root.OpenRoot(directory)
	if err != nil {
		return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	defer parent.Close()
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	temporary := tempPrefix + hex.EncodeToString(random)
	file, err := parent.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	state := storage.Unchanged
	closed := false
	published := false
	defer func() {
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
			err = failure(storage.Unavailable, storage.PutOperation, state, errors.Join(err, closeErr, removeErr))
			if removeErr != nil {
				err = err.(*storage.Error).WithCleanup(storage.NewCleanupID(directory + "/" + temporary))
			}
		}
	}()
	// A process crash releases this lock. Cleanup skips every live staging file,
	// regardless of its age; it never guesses that a slow source has finished.
	if locked, e := tryLock(file); e != nil || !locked {
		return result, failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, e)
	}
	contentType := options.ContentType
	if contentType == "" {
		contentType = storage.Binary
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
	maximum := b.config.MaxObjectBytes
	if size, present := options.Size.Get(); present {
		maximum = size
	}
	size, digest, err := storageio.Copy(ctx, file, source, maximum)
	if err != nil {
		return result, err
	}
	if expected, present := options.Size.Get(); present && expected != size {
		return result, failure(storage.IntegrityFailed, storage.PutOperation, storage.Unchanged, nil)
	}
	if expected, present := options.Checksum.Get(); present && expected != digest {
		return result, failure(storage.IntegrityFailed, storage.PutOperation, storage.Unchanged, nil)
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
	unlock, err := b.lock(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
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
	err = file.Close()
	closed = true
	if err != nil {
		return result, err
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
