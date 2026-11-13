package upload

import (
	"context"
	"crypto/sha256"
	"hash"
	"io"
	"io/fs"
	"mime"
	"os"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/mediatype"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

// Capture consumes a borrowed reader with fixed scratch space. It never closes
// the source or uses its WriterTo method. Limits include failed capture attempts.
func (b *Batch) Capture(ctx context.Context, source io.Reader, name, declaredType string) (File, error) {
	if source == nil {
		return File{}, safeerror.NewFault(fault.Invalid, "upload capture requires a reader")
	}
	return b.CaptureFrom(ctx, name, func(destination io.Writer) (string, error) {
		reader := workscope.Reader(ctx, source)
		buffer := make([]byte, bufferBytes)
		var size int64
		for {
			if err := b.ctx.Err(); err != nil {
				return "", err
			}
			allowance := len(buffer)
			if remaining := b.config.MaxFileBytes - size; remaining < int64(allowance) {
				allowance = int(remaining) + 1
			}
			n, readErr := reader.Read(buffer[:allowance])
			if n > 0 {
				if _, err := destination.Write(buffer[:n]); err != nil {
					return "", err
				}
				size += int64(n)
			}
			if readErr != nil {
				if readErr != io.EOF {
					return "", safeerror.Owned("uploaded bytes could not be read", &ReadError{Cause: readErr})
				}
				return declaredType, nil
			}
		}
	})
}

// CaptureFrom lends a bounded writer to a synchronous producer, such as an HTTP
// download. The producer returns the untrusted media hint after successful EOF.
// The writer counts, hashes and sniffs exactly the bytes stored. It becomes closed
// before this call returns; ignored write failures cannot publish a partial File.
// The batch retains ownership until the producer and all admitted writes exit.
func (b *Batch) CaptureFrom(ctx context.Context, name string, produce func(io.Writer) (string, error)) (result File, err error) {
	if b == nil {
		return File{}, fs.ErrClosed
	}
	if ctx == nil || produce == nil {
		return File{}, safeerror.NewFault(fault.Invalid, "upload capture requires a context and producer")
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
	writer := &captureWriter{batch: b, file: file, ctx: ctx, digest: sha256.New()}
	var declared string
	var produced error
	// Preserve native capture error shapes: HTTP classifies these directly after
	// removing the source-issued marker. Isolation owns only abnormal callback exit.
	abnormal := callback.Isolated("capture upload bytes", func() error {
		declared, produced = produce(writer)
		return nil
	})
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.closed = true
	if abnormal != nil {
		return File{}, abnormal
	}
	if writer.err != nil {
		return File{}, writer.err
	}
	if produced != nil {
		if _, owned := safeerror.OwnedMessage(produced); owned {
			return File{}, produced
		}
		return File{}, safeerror.Wrap("upload producer failed", produced)
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := b.ctx.Err(); err != nil {
		return File{}, err
	}
	declared, _, parseErr := mime.ParseMediaType(declared)
	if parseErr != nil {
		declared = ""
	}
	var digest [sha256.Size]byte
	copy(digest[:], writer.digest.Sum(nil))
	return File{owner: b, key: key, name: filename.Normalize(name, "upload"), declared: strings.Clone(declared), detected: mediatype.DetectUpload(writer.prefix), size: writer.size, digest: digest}, nil
}

type captureWriter struct {
	mu     sync.Mutex
	batch  *Batch
	file   *os.File
	ctx    context.Context
	digest hash.Hash
	prefix []byte
	size   int64
	closed bool
	err    error
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fs.ErrClosed
	}
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.write(p)
	w.err = err
	return n, err
}

func (w *captureWriter) write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if err := w.batch.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.batch.config.MaxFileBytes-w.size {
		return 0, safeerror.Owned("upload resource limit exceeded", &LimitError{Kind: FileBytes})
	}
	if err := w.batch.take(int64(len(p))); err != nil {
		return 0, err
	}
	n, err := w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if n > 0 {
		_, _ = w.digest.Write(p[:n])
		if len(w.prefix) < sniffBytes {
			w.prefix = append(w.prefix, p[:min(n, sniffBytes-len(w.prefix))]...)
		}
		w.size += int64(n)
	}
	return n, err
}
