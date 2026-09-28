package storage

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/weiloon1234/Foundry-Go/value"
)

// OpenSeek supplies a bounded-memory seekable view of one complete object.
// Seeking reopens a range pinned to its immutable version or strong ETag; a
// replacement fails its precondition rather than mixing two representations.
// A backend must support ranges and either version reads or conditional reads.
// Stat runs immediately; payloads open only on Read. Close the returned handle.
// Seeking concurrently with Read is serialized; Close cancels and drains Read.
func (d *Disk) OpenSeek(ctx context.Context, key ObjectKey, options ReadOptions) (io.ReadSeekCloser, ObjectInfo, error) {
	if err := d.Validate(); err != nil {
		return nil, ObjectInfo{}, err
	}
	if options.Range.IsSet() {
		return nil, ObjectInfo{}, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	if !d.capabilities.Ranges || !d.capabilities.Versions && !d.capabilities.ConditionalRead {
		return nil, ObjectInfo{}, Failure(Unsupported, OpenOperation, NotApplicable, nil)
	}
	info, err := d.Stat(ctx, key, options)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	if d.capabilities.Versions && info.Version != "" {
		options.Version = info.Version
	} else if d.capabilities.ConditionalRead && info.ETag != "" {
		options.IfMatch = info.ETag
	} else {
		return nil, ObjectInfo{}, Failure(Unsupported, OpenOperation, NotApplicable, nil)
	}
	operation, cancel := context.WithCancel(ctx)
	return &seekReader{disk: d, key: key, info: info, options: options, ctx: operation, cancel: cancel, done: make(chan struct{})}, info, nil
}

type seekReader struct {
	disk      *Disk
	key       ObjectKey
	info      ObjectInfo
	options   ReadOptions
	ctx       context.Context
	cancel    context.CancelFunc
	operation sync.Mutex
	mu        sync.Mutex
	body      io.ReadCloser
	position  int64
	closed    bool
	once      sync.Once
	done      chan struct{}
	closeErr  error
}

func (r *seekReader) Read(p []byte) (int, error) {
	r.operation.Lock()
	defer r.operation.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, Failure(Closed, OpenOperation, NotApplicable, nil)
	}
	body := r.body
	r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return 0, Failure(Unavailable, OpenOperation, NotApplicable, err)
	}
	if body == nil {
		if r.position >= r.info.Size {
			return 0, io.EOF
		}
		options := r.options
		if r.position > 0 {
			options.Range = value.Set(ByteRange{Offset: r.position, Length: r.info.Size - r.position})
		}
		opened, info, err := r.disk.Open(r.ctx, r.key, options)
		if err != nil {
			return 0, err
		}
		if info.Object.Size != r.info.Size || info.Object.ETag != r.info.ETag || info.Object.Version != r.info.Version {
			return 0, Failure(PreconditionFailed, OpenOperation, NotApplicable, closeBody(opened))
		}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return 0, Failure(Closed, OpenOperation, NotApplicable, closeBody(opened))
		}
		r.body, body = opened, opened
		r.mu.Unlock()
	}
	n, err := body.Read(p)
	r.position += int64(n)
	return n, err
}
func (r *seekReader) Seek(offset int64, whence int) (int64, error) {
	r.operation.Lock()
	defer r.operation.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, Failure(Closed, OpenOperation, NotApplicable, nil)
	}
	if err := r.ctx.Err(); err != nil {
		return 0, Failure(Unavailable, OpenOperation, NotApplicable, err)
	}
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.position
	case io.SeekEnd:
		base = r.info.Size
	default:
		return 0, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	if offset > 0 && base > math.MaxInt64-offset || offset < -base {
		return 0, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	next := base + offset
	if next == r.position {
		return next, nil
	}
	if r.body != nil {
		err := closeBody(r.body)
		r.body = nil
		if err != nil {
			return 0, Failure(Unavailable, OpenOperation, NotApplicable, err)
		}
	}
	r.position = next
	return next, nil
}
func (r *seekReader) Close() error {
	r.once.Do(func() {
		r.cancel()
		r.mu.Lock()
		r.closed = true
		body := r.body
		r.body = nil
		r.mu.Unlock()
		if body != nil {
			r.closeErr = closeBody(body)
		}
		r.operation.Lock()
		r.operation.Unlock()
		if r.closeErr != nil {
			r.closeErr = Failure(Unavailable, CloseOperation, NotApplicable, errors.Join(r.closeErr))
		}
		close(r.done)
	})
	<-r.done
	return r.closeErr
}
