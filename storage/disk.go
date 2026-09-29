package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

// streamTimeout is the cancellation cause when a read stream does not open
// within Timeout or makes no read progress for StreamIdleTimeout.
var streamTimeout = fault.Wrap(fault.Timeout, "storage stream made no progress before its deadline", context.DeadlineExceeded)

// Disk owns bounded operations and returned readers, borrowing its backend.
// Metadata/write operations and open read streams use separate queued
// admission pools. Close cancels work and closes readers; Done waits for their
// actual release. Close the backend only afterward. Construction performs no
// storage I/O.
type Disk struct {
	id           DiskID
	backend      Backend
	config       Config
	capabilities Capabilities
	operations   *admission.Semaphore
	streams      *admission.Semaphore
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	active       int
	streaming    int
	closing      bool
	readers      map[*ownedReader]struct{}
	done         chan struct{}
	closeErr     error
}

func NewDisk(id DiskID, backend Backend, config Config) (*Disk, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	config = config.normalized()
	if backend == nil {
		return nil, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	var capabilities Capabilities
	if err := callback.Isolated("storage capabilities", func() error { capabilities = backend.Capabilities(); return nil }); err != nil {
		return nil, Failure(Invalid, OpenOperation, NotApplicable, err)
	}
	if capabilities.ConditionalWriteMaxBytes < 0 {
		return nil, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Disk{id: id, backend: backend, config: config, capabilities: capabilities, operations: admission.New(config.MaxActive), streams: admission.New(config.MaxStreams), ctx: ctx, cancel: cancel, readers: make(map[*ownedReader]struct{}), done: make(chan struct{})}, nil
}
func (d *Disk) ID() DiskID {
	if d == nil {
		return ""
	}
	return d.id
}
func (d *Disk) Capabilities() Capabilities {
	if d == nil {
		return Capabilities{}
	}
	return d.capabilities
}

// Config returns the disk's normalized limits. Recovery policies use Timeout to
// bound how long an admitted operation can still be running.
func (d *Disk) Config() Config {
	if d == nil {
		return Config{}
	}
	return d.config
}
func (d *Disk) Validate() error {
	if d == nil || d.done == nil {
		return Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	return nil
}

// admit waits in FIFO order for a slot of pool. Exhausted capacity is a
// retryable Unavailable failure whose cause matches fault.Overloaded; the
// operation did not start. Closing the disk ends every wait as Closed.
func (d *Disk) admit(ctx context.Context, pool *admission.Semaphore, op Operation) (func(), error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, Failure(Invalid, op, NotApplicable, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, Failure(Unavailable, op, Unchanged, err)
	}
	d.mu.Lock()
	closing := d.closing
	d.mu.Unlock()
	if closing {
		return nil, Failure(Closed, op, Unchanged, nil)
	}
	if err := pool.Acquire(ctx, admission.Wait(d.config.Timeout), d.ctx.Done()); err != nil {
		if errors.Is(err, fault.Closed) {
			return nil, Failure(Closed, op, Unchanged, err)
		}
		return nil, Failure(Unavailable, op, Unchanged, err)
	}
	streaming := pool == d.streams
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		pool.Release()
		return nil, Failure(Closed, op, Unchanged, nil)
	}
	d.active++
	if streaming {
		d.streaming++
	}
	d.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.active--
			if streaming {
				d.streaming--
			}
			pool.Release()
			if d.closing && d.active == 0 {
				close(d.done)
			}
		})
	}, nil
}
func (d *Disk) begin(ctx context.Context, op Operation) (context.Context, func(), error) {
	admitted, err := d.admit(ctx, d.operations, op)
	if err != nil {
		return nil, nil, err
	}
	operation, cancel := context.WithTimeout(ctx, d.config.Timeout)
	stop := context.AfterFunc(d.ctx, cancel)
	var once sync.Once
	release := func() {
		once.Do(func() {
			stop()
			cancel()
			admitted()
		})
	}
	return operation, release, nil
}
func finish(op Operation, ctx context.Context, err error, success Outcome) error {
	if err == nil && ctx.Err() == nil {
		return nil
	}
	var result error
	failed := callback.Isolated("classify storage outcome", func() error {
		result = classifyOutcome(op, ctx, err, success)
		return nil
	})
	if failed == nil {
		return result
	}
	outcome := Unknown
	if success == NotApplicable {
		outcome = NotApplicable
	}
	return Failure(Unavailable, op, outcome, errors.Join(context.Cause(ctx), failed))
}

func classifyOutcome(op Operation, ctx context.Context, err error, success Outcome) error {
	// Classify once inside finish's callback isolation. A cyclic/deep/wide
	// graph without a reached storage error cannot establish a mutation outcome.
	var failure *Error
	errorgraph.Walk(err, func(current error) bool {
		detail, matched := errorgraph.AsShallow[*Error](current)
		if matched {
			failure = detail
		}
		return !matched
	})
	if ctx.Err() != nil {
		canceled := context.Cause(ctx)
		outcome := success
		if err != nil {
			outcome = Unknown
			if failure != nil {
				outcome = failure.Outcome()
			}
		}
		result := Failure(Unavailable, op, outcome, errors.Join(canceled, err))
		if failure != nil {
			if id, present := failure.Cleanup().Get(); present {
				result = result.WithCleanup(id)
			}
		}
		return result
	}
	if err == nil {
		return nil
	}
	if failure != nil {
		safe := Failure(failure.Code(), op, failure.Outcome(), err)
		if id, present := failure.Cleanup().Get(); present {
			safe = safe.WithCleanup(id)
		}
		return safe
	}
	outcome := Unknown
	if success == NotApplicable {
		outcome = NotApplicable
	}
	return Failure(Unavailable, op, outcome, err)
}
func (d *Disk) writeOptions(o PutOptions) error {
	if err := o.Validate(d.config.MaxObjectBytes); err != nil {
		return err
	}
	return d.capabilities.ValidatePut(o)
}
func (d *Disk) readOptions(o ReadOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}
	return d.capabilities.ValidateRead(o)
}
func (d *Disk) validateInfo(key ObjectKey, info ObjectInfo) error {
	if err := info.Validate(); err != nil {
		return err
	}
	if info.Key != key || info.Size > d.config.MaxObjectBytes {
		return Failure(IntegrityFailed, StatOperation, NotApplicable, nil)
	}
	return nil
}
func (d *Disk) Put(ctx context.Context, key ObjectKey, source io.Reader, options PutOptions) (StoredObject, error) {
	if err := d.Validate(); err != nil {
		return StoredObject{}, err
	}
	if err := key.Validate(); err != nil {
		return StoredObject{}, err
	}
	if source == nil {
		return StoredObject{}, Failure(Invalid, PutOperation, Unchanged, nil)
	}
	if err := d.writeOptions(options); err != nil {
		return StoredObject{}, err
	}
	options.Metadata = options.Metadata.Clone()
	op, release, err := d.begin(ctx, PutOperation)
	if err != nil {
		return StoredObject{}, err
	}
	defer release()
	// Adapters receive the disk's hard maximum even for unknown input length via
	// a guarded source. Returning early without consuming EOF cannot claim success.
	bounded := &putReader{ctx: op, source: source, maximum: d.config.MaxObjectBytes}
	var info ObjectInfo
	err = callback.Isolated("storage put", func() error { var err error; info, err = d.backend.Put(op, key, bounded, options); return err })
	if err == nil {
		if !bounded.eof || bounded.failed != nil {
			err = Failure(IntegrityFailed, PutOperation, Unknown, bounded.failed)
		} else if e := d.validateInfo(key, info); e != nil {
			err = Failure(IntegrityFailed, PutOperation, Applied, e)
		} else if info.Size != bounded.read {
			err = Failure(IntegrityFailed, PutOperation, Applied, nil)
		} else if expected, present := options.Size.Get(); present && info.Size != expected {
			err = Failure(IntegrityFailed, PutOperation, Applied, nil)
		} else if expected, present := options.Checksum.Get(); present {
			actual, available := info.Checksum.Get()
			if !available || actual != expected {
				err = Failure(IntegrityFailed, PutOperation, Applied, nil)
			}
		}
	}
	if err = finish(PutOperation, op, err, Applied); err != nil {
		return StoredObject{}, err
	}
	return StoredObject{Disk: d.id, Object: info}, nil
}
func (d *Disk) PutBytes(ctx context.Context, key ObjectKey, data []byte, options PutOptions) (StoredObject, error) {
	if size, present := options.Size.Get(); present && size != int64(len(data)) {
		return StoredObject{}, Failure(Invalid, PutOperation, Unchanged, nil)
	}
	options.Size = value.Set(int64(len(data)))
	return d.Put(ctx, key, bytes.NewReader(data), options)
}

// Open returns an owned reader holding one MaxStreams slot until Close,
// cancellation, shutdown or StreamIdleTimeout without read progress. Opening
// itself must complete within Timeout. A stream is not bounded by Timeout, so a
// slow consumer can finish a large download while it keeps reading.
func (d *Disk) Open(ctx context.Context, key ObjectKey, options ReadOptions) (io.ReadCloser, ReadInfo, error) {
	if err := d.Validate(); err != nil {
		return nil, ReadInfo{}, err
	}
	if err := key.Validate(); err != nil {
		return nil, ReadInfo{}, err
	}
	if err := d.readOptions(options); err != nil {
		return nil, ReadInfo{}, err
	}
	admitted, err := d.admit(ctx, d.streams, OpenOperation)
	if err != nil {
		return nil, ReadInfo{}, err
	}
	// Reader.Close must synchronously cancel the context given to the backend
	// before interrupting its body. Disk cancellation reaches operation contexts
	// through AfterFunc, whose scheduling must not determine a read's outcome.
	op, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(d.ctx, func() { cancel(context.Canceled) })
	watchdog := time.AfterFunc(d.config.Timeout, func() { cancel(streamTimeout) })
	var once sync.Once
	release := func() {
		once.Do(func() {
			watchdog.Stop()
			stop()
			cancel(context.Canceled)
			admitted()
		})
	}
	var body io.ReadCloser
	var info ReadInfo
	err = callback.Isolated("storage open", func() error { var err error; body, info, err = d.backend.Open(op, key, options); return err })
	if err == nil {
		err = d.validateInfo(key, info.Object)
		offset, length := int64(0), info.Object.Size
		if requested, present := options.Range.Get(); present {
			var rangeErr error
			offset, length, rangeErr = requested.Resolve(info.Object.Size)
			err = errors.Join(err, rangeErr)
		}
		if body == nil || info.Offset != offset || info.Length != length {
			err = errors.Join(err, Failure(IntegrityFailed, OpenOperation, NotApplicable, nil))
		}
	}
	err = finish(OpenOperation, op, err, NotApplicable)
	if err != nil {
		cancel(context.Canceled)
		if body != nil {
			if cleanup := closeBody(body); cleanup != nil {
				err = Failure(Unavailable, OpenOperation, NotApplicable, errors.Join(err, cleanup))
			}
		}
		release()
		return nil, ReadInfo{}, err
	}
	// Opening succeeded: the watchdog now measures read progress only.
	watchdog.Reset(d.config.StreamIdleTimeout)
	reader := &ownedReader{disk: d, raw: body, ctx: op, cancel: func() { cancel(context.Canceled) }, release: release, idle: watchdog, idleTimeout: d.config.StreamIdleTimeout, length: info.Length, closed: make(chan struct{})}
	reader.verifyChecksum(info)
	d.mu.Lock()
	d.readers[reader] = struct{}{}
	closing := d.closing
	d.mu.Unlock()
	reader.watchMu.Lock()
	reader.watch = context.AfterFunc(op, func() { _ = reader.Close() })
	reader.watchMu.Unlock()
	if closing || op.Err() != nil {
		cause := context.Cause(op)
		cleanup := reader.Close()
		return nil, ReadInfo{}, Failure(Closed, OpenOperation, NotApplicable, errors.Join(cause, cleanup))
	}
	return reader, info, nil
}
func (d *Disk) Stat(ctx context.Context, key ObjectKey, options ReadOptions) (ObjectInfo, error) {
	if err := d.Validate(); err != nil {
		return ObjectInfo{}, err
	}
	if err := key.Validate(); err != nil {
		return ObjectInfo{}, err
	}
	if err := d.readOptions(options); err != nil {
		return ObjectInfo{}, err
	}
	if options.Range.IsSet() {
		return ObjectInfo{}, Failure(Invalid, StatOperation, NotApplicable, nil)
	}
	op, release, err := d.begin(ctx, StatOperation)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer release()
	var info ObjectInfo
	err = callback.Isolated("storage stat", func() error { var err error; info, err = d.backend.Stat(op, key, options); return err })
	if err == nil {
		err = d.validateInfo(key, info)
	}
	if err = finish(StatOperation, op, err, NotApplicable); err != nil {
		return ObjectInfo{}, err
	}
	return info, nil
}
func (d *Disk) Exists(ctx context.Context, key ObjectKey) (bool, error) {
	_, err := d.Stat(ctx, key, ReadOptions{})
	if err == nil {
		return true, nil
	}
	var missing *Error
	if errors.As(err, &missing) && missing.Code() == NotFound {
		return false, nil
	}
	return false, err
}
func (d *Disk) Delete(ctx context.Context, key ObjectKey, options DeleteOptions) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	if err := d.capabilities.ValidateDelete(options); err != nil {
		return err
	}
	op, release, err := d.begin(ctx, DeleteOperation)
	if err != nil {
		return err
	}
	defer release()
	err = callback.Isolated("storage delete", func() error { return d.backend.Delete(op, key, options) })
	return finish(DeleteOperation, op, err, Applied)
}
func (d *Disk) List(ctx context.Context, options ListOptions) (Page, error) {
	if err := d.Validate(); err != nil {
		return Page{}, err
	}
	if err := options.Validate(); err != nil {
		return Page{}, err
	}
	if err := d.capabilities.ValidateList(options); err != nil {
		return Page{}, err
	}
	op, release, err := d.begin(ctx, ListOperation)
	if err != nil {
		return Page{}, err
	}
	defer release()
	var page Page
	err = callback.Isolated("storage list", func() error { var err error; page, err = d.backend.List(op, options); return err })
	if err == nil {
		err = validatePage(options, page)
	}
	if err = finish(ListOperation, op, err, NotApplicable); err != nil {
		return Page{}, err
	}
	// An adapter's own object bound may exceed this disk's. Such entries are
	// counted, never returned as objects this disk would refuse to read.
	objects := make([]ObjectInfo, 0, len(page.Objects))
	for _, info := range page.Objects {
		if info.Size > d.config.MaxObjectBytes {
			page.Skipped++
			continue
		}
		objects = append(objects, info)
	}
	page.Objects = objects
	page.Directories = append([]Prefix(nil), page.Directories...)
	return page, nil
}

// validatePage checks an adapter page as an untrusted boundary: bounds, order,
// prefix containment and one-level shape for delimited listings.
func validatePage(options ListOptions, page Page) error {
	integrity := func(cause error) error { return Failure(IntegrityFailed, ListOperation, NotApplicable, cause) }
	if len(page.Objects)+len(page.Directories) > options.Limit || page.Skipped < 0 || len(page.Next.Token()) > MaxCursorBytes || !page.Next.IsZero() && page.Next == options.Cursor {
		return integrity(nil)
	}
	if !options.Delimited && len(page.Directories) > 0 {
		return integrity(nil)
	}
	prefix := options.Prefix.String()
	previous := ""
	for _, info := range page.Objects {
		if err := info.ValidateListed(); err != nil {
			return integrity(err)
		}
		text := info.Key.String()
		if !options.Prefix.Contains(info.Key) || text <= previous || options.Delimited && strings.Contains(text[len(prefix):], "/") {
			return integrity(nil)
		}
		previous = text
	}
	previous = ""
	for _, directory := range page.Directories {
		text := directory.String()
		if directory.Validate() != nil || !strings.HasPrefix(text, prefix) || len(text) <= len(prefix)+1 || !strings.HasSuffix(text, "/") || strings.Contains(text[len(prefix):len(text)-1], "/") || text <= previous {
			return integrity(nil)
		}
		previous = text
	}
	return nil
}
func (d *Disk) Close(ctx context.Context) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return Failure(Invalid, CloseOperation, NotApplicable, nil)
	}
	d.mu.Lock()
	if !d.closing {
		d.closing = true
		d.cancel()
		for reader := range d.readers {
			go func() { _ = reader.Close() }()
		}
		if d.active == 0 {
			close(d.done)
		}
	}
	d.mu.Unlock()
	select {
	case <-d.done:
		return d.closeErr
	default:
	}
	select {
	case <-d.done:
		return d.closeErr
	case <-ctx.Done():
		return Failure(Unavailable, CloseOperation, NotApplicable, ctx.Err())
	}
}
func (d *Disk) Done() <-chan struct{} {
	if d == nil {
		return nil
	}
	return d.done
}

// Stats reports admitted work. Active counts operations and open streams;
// Streams is the subset holding MaxStreams slots.
type Stats struct {
	Active  int
	Streams int
	Closing bool
}

func (d *Disk) Stats() Stats {
	if d == nil {
		return Stats{Closing: true}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return Stats{Active: d.active, Streams: d.streaming, Closing: d.closing}
}
