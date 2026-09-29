package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type putReader struct {
	ctx           context.Context
	source        io.Reader
	maximum, read int64
	eof           bool
	failed        error
}

func (r *putReader) Read(p []byte) (int, error) {
	if r.failed != nil {
		return 0, r.failed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		r.failed = err
		return 0, err
	}
	if r.eof {
		return 0, io.EOF
	}
	maximum := int(min(int64(len(p)), r.maximum-r.read+1))
	n, err := r.source.Read(p[:maximum])
	if n < 0 || n > maximum {
		r.failed = Failure(IntegrityFailed, PutOperation, Unchanged, nil)
		return 0, r.failed
	}
	if int64(n) > r.maximum-r.read {
		r.failed = Failure(LimitExceeded, PutOperation, Unchanged, nil)
		return 0, r.failed
	}
	r.read += int64(n)
	if err != nil {
		if err == io.EOF {
			r.eof = true
		} else {
			r.failed = err
		}
	}
	if canceled := r.ctx.Err(); canceled != nil {
		r.failed = errors.Join(canceled, err)
		return n, r.failed
	}
	return n, err
}
func closeBody(body io.Closer) error { return callback.Isolated("storage reader close", body.Close) }

type ownedReader struct {
	disk         *Disk
	raw          io.ReadCloser
	ctx          context.Context
	cancel       func()
	release      func()
	idle         *time.Timer
	idleTimeout  time.Duration
	watch        func() bool
	watchMu      sync.Mutex
	length, read int64
	readMu       sync.Mutex
	once         sync.Once
	closing      atomic.Bool
	closed       chan struct{}
	closeErr     error
	digest       hash.Hash
	checksum     SHA256
}

func (r *ownedReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.closing.Load() {
		return 0, Failure(Closed, OpenOperation, NotApplicable, nil)
	}
	if r.ctx.Err() != nil {
		return 0, Failure(Unavailable, OpenOperation, NotApplicable, context.Cause(r.ctx))
	}
	allowed := int(min(int64(len(p)), r.length-r.read+1))
	var returned error
	err = callback.Isolated("storage reader read", func() error { n, returned = r.raw.Read(p[:allowed]); return nil })
	if err == nil {
		err = returned
	}
	if n < 0 || n > allowed || int64(n) > r.length-r.read {
		return 0, Failure(IntegrityFailed, OpenOperation, NotApplicable, nil)
	}
	r.read += int64(n)
	if n > 0 {
		// Progress restarts the idle deadline; a stalled provider or consumer
		// that stops reading is cancelled after StreamIdleTimeout.
		r.idle.Reset(r.idleTimeout)
		if r.digest != nil {
			_, _ = r.digest.Write(p[:n])
		}
	}
	if err == io.EOF && r.read == r.length && r.digest != nil {
		var actual SHA256
		copy(actual[:], r.digest.Sum(nil))
		if actual != r.checksum {
			err = Failure(IntegrityFailed, OpenOperation, NotApplicable, nil)
		}
	}
	if err == io.EOF && r.read != r.length {
		err = io.ErrUnexpectedEOF
	}
	if r.ctx.Err() != nil {
		return n, Failure(Unavailable, OpenOperation, NotApplicable, errors.Join(context.Cause(r.ctx), err))
	}
	if err != nil && err != io.EOF {
		return n, finish(OpenOperation, r.ctx, err, NotApplicable)
	}
	return n, err
}
func (r *ownedReader) Close() error {
	r.once.Do(func() {
		r.closing.Store(true)
		r.watchMu.Lock()
		if r.watch != nil {
			r.watch()
		}
		r.watchMu.Unlock()
		r.cancel()
		if err := closeBody(r.raw); err != nil {
			r.closeErr = Failure(Unavailable, CloseOperation, NotApplicable, err)
		}
		r.readMu.Lock()
		r.readMu.Unlock()
		r.disk.mu.Lock()
		delete(r.disk.readers, r)
		if r.closeErr != nil {
			r.disk.closeErr = errors.Join(r.disk.closeErr, Failure(Unavailable, CloseOperation, NotApplicable, r.closeErr))
		}
		r.disk.mu.Unlock()
		r.release()
		close(r.closed)
	})
	<-r.closed
	return r.closeErr
}

func (r *ownedReader) verifyChecksum(info ReadInfo) {
	if checksum, available := info.Object.Checksum.Get(); available && info.Offset == 0 && info.Length == info.Object.Size {
		r.digest, r.checksum = sha256.New(), checksum
	}
}
