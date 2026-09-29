// Package storageio owns bounded transfer and hostile-reader handling for
// storage adapters. Publication and cleanup remain the adapter's responsibility.
package storageio

import (
	"context"
	"crypto/sha256"
	"io"
	"sync"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const BufferBytes = 32 << 10

// buffers reuses transfer buffers across copies; each copy owns one buffer
// until it returns.
var buffers = sync.Pool{New: func() any { return new([BufferBytes]byte) }}

// Copy reads at most maximum+1 source bytes and never writes beyond the bound.
// It does not close either stream or use ReaderFrom/WriterTo fast paths that could
// evade its limits. EOF after bytes is valid; a non-EOF error with bytes is fatal.
func Copy(ctx context.Context, dst io.Writer, source io.Reader, maximum int64) (size int64, digest storage.SHA256, err error) {
	if ctx == nil || dst == nil || source == nil || maximum < 0 {
		return 0, digest, storage.Failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	hash := sha256.New()
	pooled := buffers.Get().(*[BufferBytes]byte)
	defer buffers.Put(pooled)
	err = callback.Isolated("storage byte transfer", func() error {
		buffer := pooled[:]
		empty := 0
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			allowance := len(buffer)
			if remaining := maximum - size; remaining < int64(allowance) {
				allowance = int(remaining) + 1
			}
			n, readErr := source.Read(buffer[:allowance])
			if n < 0 || n > allowance {
				return storage.Failure(storage.IntegrityFailed, storage.PutOperation, storage.Unchanged, nil)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if int64(n) > maximum-size {
				return storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
			}
			if n > 0 {
				empty = 0
				written, writeErr := dst.Write(buffer[:n])
				if written < 0 || written > n {
					return io.ErrShortWrite
				}
				if written != n && writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				if writeErr != nil {
					return writeErr
				}
				_, _ = hash.Write(buffer[:n])
				size += int64(n)
			} else if readErr == nil {
				empty++
				if empty >= 100 {
					return io.ErrNoProgress
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					return readErr
				}
				return ctx.Err()
			}
		}
	})
	if err != nil {
		return 0, storage.SHA256{}, err
	}
	copy(digest[:], hash.Sum(nil))
	return size, digest, nil
}
