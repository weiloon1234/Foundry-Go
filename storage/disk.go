package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Disk owns bounded operations and returned readers, borrowing its backend.
// Close cancels work and closes readers; Done waits for their actual release.
// Close the backend only afterward. Construction performs no storage I/O.
type Disk struct {
	id           DiskID
	backend      Backend
	config       Config
	capabilities Capabilities
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	active       int
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
	return &Disk{id: id, backend: backend, config: config, capabilities: capabilities, ctx: ctx, cancel: cancel, readers: make(map[*ownedReader]struct{}), done: make(chan struct{})}, nil
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
func (d *Disk) Validate() error {
	if d == nil || d.done == nil {
		return Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	return nil
}
func (d *Disk) begin(ctx context.Context, op Operation) (context.Context, func(), error) {
	if err := d.Validate(); err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		return nil, nil, Failure(Invalid, op, NotApplicable, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, Failure(Unavailable, op, Unchanged, err)
	}
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		return nil, nil, Failure(Closed, op, Unchanged, nil)
	}
	if d.active >= d.config.MaxActive {
		d.mu.Unlock()
		return nil, nil, Failure(LimitExceeded, op, Unchanged, nil)
	}
	d.active++
	d.mu.Unlock()
	operation, cancel := context.WithTimeout(ctx, d.config.Timeout)
	stop := context.AfterFunc(d.ctx, cancel)
	var once sync.Once
	release := func() {
		once.Do(func() {
			stop()
			cancel()
			d.mu.Lock()
			defer d.mu.Unlock()
			d.active--
			if d.closing && d.active == 0 {
				close(d.done)
			}
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
	return Failure(Unavailable, op, outcome, errors.Join(ctx.Err(), failed))
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
	if canceled := ctx.Err(); canceled != nil {
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
	c := d.capabilities
	if o.Range.IsSet() && !c.Ranges || o.IfMatch != "" && !c.ConditionalRead || o.Version != "" && !c.Versions {
		return Failure(Unsupported, OpenOperation, NotApplicable, nil)
	}
	return nil
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
	op, release, err := d.begin(ctx, OpenOperation)
	if err != nil {
		return nil, ReadInfo{}, err
	}
	// Reader.Close must synchronously cancel the context given to the backend
	// before interrupting its body. Disk cancellation reaches operation contexts
	// through AfterFunc, whose scheduling must not determine a read's outcome.
	op, cancel := context.WithCancel(op)
	releaseAdmission := release
	release = func() { cancel(); releaseAdmission() }
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
		cancel()
		if body != nil {
			if cleanup := closeBody(body); cleanup != nil {
				err = Failure(Unavailable, OpenOperation, NotApplicable, errors.Join(err, cleanup))
			}
		}
		release()
		return nil, ReadInfo{}, err
	}
	reader := &ownedReader{disk: d, raw: body, ctx: op, cancel: cancel, release: release, length: info.Length, closed: make(chan struct{})}
	reader.verifyChecksum(info)
	d.mu.Lock()
	d.readers[reader] = struct{}{}
	closing := d.closing
	d.mu.Unlock()
	reader.watchMu.Lock()
	reader.watch = context.AfterFunc(op, func() { _ = reader.Close() })
	reader.watchMu.Unlock()
	if closing || op.Err() != nil {
		cleanup := reader.Close()
		return nil, ReadInfo{}, Failure(Closed, OpenOperation, NotApplicable, errors.Join(op.Err(), cleanup))
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
	if options.IfMatch != "" && !d.capabilities.ConditionalDelete || options.Version != "" && !d.capabilities.Versions {
		return Failure(Unsupported, DeleteOperation, Unchanged, nil)
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
	op, release, err := d.begin(ctx, ListOperation)
	if err != nil {
		return Page{}, err
	}
	defer release()
	var page Page
	err = callback.Isolated("storage list", func() error { var err error; page, err = d.backend.List(op, options); return err })
	if err == nil {
		if len(page.Objects) > options.Limit || len(page.Next.Token()) > MaxCursorBytes || !page.Next.IsZero() && page.Next == options.Cursor {
			err = Failure(IntegrityFailed, ListOperation, NotApplicable, nil)
		}
		previous := ""
		for _, info := range page.Objects {
			if e := d.validateInfo(info.Key, info); e != nil {
				err = errors.Join(err, e)
			}
			if !options.Prefix.Contains(info.Key) || info.Key.String() <= previous {
				err = Failure(IntegrityFailed, ListOperation, NotApplicable, err)
			}
			previous = info.Key.String()
		}
	}
	if err = finish(ListOperation, op, err, NotApplicable); err != nil {
		return Page{}, err
	}
	page.Objects = append([]ObjectInfo(nil), page.Objects...)
	return page, nil
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

type Stats struct {
	Active  int
	Closing bool
}

func (d *Disk) Stats() Stats {
	if d == nil {
		return Stats{Closing: true}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return Stats{Active: d.active, Closing: d.closing}
}
