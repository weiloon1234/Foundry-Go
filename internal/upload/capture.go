package upload

import (
	"context"
	"io"
	"io/fs"
	"mime"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Capture stores bytes with a fixed scratch buffer. Limits account for attempted
// writes and file creation; failed attempts do not replenish the request budget.
// The caller still owns the incoming reader. No partial File is published.
func (b *Batch) Capture(ctx context.Context, source io.Reader, name, declaredType string) (result File, err error) {
	if b == nil {
		return File{}, fs.ErrClosed
	}
	if ctx == nil || source == nil {
		return File{}, safeerror.NewFault(fault.Invalid, "upload capture requires a context and reader")
	}
	file, key, err := b.create(ctx)
	if err != nil {
		return File{}, err
	}
	defer b.end()
	defer func() {
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			result = File{}
			removeErr := b.remove(key)
			if closeErr != nil || removeErr != nil {
				err = safeerror.Owned("upload cleanup failed", &CleanupError{Cause: safeerror.Join("operation failed", err, closeErr, removeErr)})
			}
		}
	}()
	var size int64
	var prefix []byte
	var failed error
	owned := callback.Isolated("capture upload bytes", func() error {
		buffer := make([]byte, bufferBytes)
		empty := 0
		for {
			if err := ctx.Err(); err != nil {
				failed = err
				return nil
			}
			if err := b.ctx.Err(); err != nil {
				failed = err
				return nil
			}
			n, readErr := source.Read(buffer)
			if n < 0 || n > len(buffer) {
				failed = safeerror.NewFault(fault.Internal, "upload reader returned an invalid byte count")
				return nil
			}
			if n > 0 {
				empty = 0
				if int64(n) > b.config.MaxFileBytes-size {
					failed = safeerror.Owned("upload resource limit exceeded", &LimitError{Kind: FileBytes})
					return nil
				}
				if err := b.take(int64(n)); err != nil {
					failed = err
					return nil
				}
				if len(prefix) < sniffBytes {
					prefix = append(prefix, buffer[:min(n, sniffBytes-len(prefix))]...)
				}
				written, writeErr := file.Write(buffer[:n])
				if writeErr == nil && written != n {
					writeErr = io.ErrShortWrite
				}
				if writeErr != nil {
					failed = writeErr
					return nil
				}
				size += int64(n)
			} else if readErr == nil {
				empty++
				if empty >= 100 {
					failed = safeerror.Owned("uploaded bytes could not be read", &ReadError{Cause: io.ErrNoProgress})
					return nil
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					failed = safeerror.Owned("uploaded bytes could not be read", &ReadError{Cause: readErr})
				}
				return nil
			}
		}
	})
	if owned != nil {
		return File{}, owned
	}
	if failed != nil {
		return File{}, failed
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := b.ctx.Err(); err != nil {
		return File{}, err
	}
	declared, _, parseErr := mime.ParseMediaType(declaredType)
	if parseErr != nil {
		declared = ""
	}
	return File{owner: b, key: key, name: filename.Normalize(name, "upload"), declared: strings.Clone(declared), detected: stdhttp.DetectContentType(prefix), size: size}, nil
}
