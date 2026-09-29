package local

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/weiloon1234/Foundry-Go/storage"
)

func (b *Backend) readOptions(options storage.ReadOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	return b.Capabilities().ValidateRead(options)
}
func (b *Backend) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	if err := b.ready(ctx, storage.OpenOperation); err != nil {
		return nil, storage.ReadInfo{}, err
	}
	if err := key.Validate(); err != nil {
		return nil, storage.ReadInfo{}, err
	}
	if err := b.readOptions(options); err != nil {
		return nil, storage.ReadInfo{}, err
	}
	file, info, dataOffset, err := b.openRecord(key)
	if err != nil {
		return nil, storage.ReadInfo{}, err
	}
	offset, length := int64(0), info.Size
	if options.IfMatch != "" && options.IfMatch != info.ETag {
		err = failure(storage.PreconditionFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if requested, present := options.Range.Get(); present && err == nil {
		offset, length, err = requested.Resolve(info.Size)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, storage.ReadInfo{}, failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, errors.Join(err, file.Close()))
	}
	reader := &objectReader{file: file, ctx: ctx, section: io.NewSectionReader(file, dataOffset+offset, length), length: length}
	return reader, storage.ReadInfo{Object: info, Offset: offset, Length: length}, nil
}
func (b *Backend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	if options.Range.IsSet() {
		return storage.ObjectInfo{}, failure(storage.Invalid, storage.StatOperation, storage.NotApplicable, nil)
	}
	reader, info, err := b.Open(ctx, key, options)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if err = reader.Close(); err != nil {
		return storage.ObjectInfo{}, err
	}
	return info.Object, nil
}

type objectReader struct {
	file         *os.File
	section      *io.SectionReader
	ctx          context.Context
	length, read int64
	mu           sync.Mutex
	once         sync.Once
	closeErr     error
}

func (r *objectReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return 0, failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, err)
	}
	n, err := r.section.Read(p)
	r.read += int64(n)
	if err == io.EOF {
		if r.read != r.length {
			return n, failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, io.ErrUnexpectedEOF)
		}
	}
	if canceled := r.ctx.Err(); canceled != nil {
		return n, failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, errors.Join(canceled, err))
	}
	return n, err
}
func (r *objectReader) Close() error {
	r.once.Do(func() { r.closeErr = failureIf(r.file.Close(), storage.CloseOperation, storage.NotApplicable) })
	return r.closeErr
}
func (b *Backend) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) (err error) {
	if err = b.ready(ctx, storage.DeleteOperation); err != nil {
		return err
	}
	if err = key.Validate(); err != nil {
		return err
	}
	if err = options.Validate(); err != nil {
		return err
	}
	if err = b.Capabilities().ValidateDelete(options); err != nil {
		return err
	}
	unlock, err := b.lockKey(ctx, key)
	if err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	file, info, _, err := b.openRecord(key)
	if err != nil {
		if errors.Is(err, storage.NotFound) {
			if options.IfMatch != "" {
				return failure(storage.PreconditionFailed, storage.DeleteOperation, storage.Unchanged, nil)
			}
			return nil
		}
		return err
	}
	if err = file.Close(); err != nil {
		return failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, err)
	}
	if options.IfMatch != "" && options.IfMatch != info.ETag {
		return failure(storage.PreconditionFailed, storage.DeleteOperation, storage.Unchanged, nil)
	}
	if err = ctx.Err(); err != nil {
		return failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, err)
	}
	dir, name := address(key)
	parent, err := b.root.OpenRoot(dir)
	if err != nil {
		return failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, err)
	}
	defer parent.Close()
	if err = parent.Remove(name); err != nil {
		return failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, err)
	}
	// An unconditional removal is decided and need not block the shard during
	// its durability sync; a conditional one holds it until the removal is durable.
	if options.IfMatch == "" {
		unlock()
		locked = false
	}
	if b.beforeSync != nil {
		b.beforeSync(key)
	}
	if b.config.Sync {
		if err = syncDirectory(parent); err != nil {
			return failure(storage.Unavailable, storage.DeleteOperation, storage.Applied, err)
		}
	}
	return failureIf(ctx.Err(), storage.DeleteOperation, storage.Applied)
}
