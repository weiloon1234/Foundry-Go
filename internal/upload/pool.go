package upload

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// PoolConfig reserves capture slots and bytes independently of downstream work.
// Directory must be a private, writable directory on a quota-controlled volume.
// Claim reconciles captures left by a dead owner before enabling reservations.
type PoolConfig struct {
	Directory string
	MaxActive int
	MaxBytes  int64
}

func (c PoolConfig) Validate() error {
	if !filepath.IsAbs(c.Directory) || c.MaxActive <= 0 || c.MaxActive > 1<<20 || c.MaxBytes <= 0 {
		return safeerror.NewFault(fault.Invalid, "upload pool requires an absolute directory and positive capacity")
	}
	return nil
}

// Pool owns one directory and a fail-fast reservation budget. Close only after
// all borrowers exit; retained cleanup can be retried without admitting work.
type Pool struct {
	config   PoolConfig
	slots    *admission.Semaphore
	mu       sync.Mutex
	cleanup  sync.Mutex
	bytes    int64
	retained map[*Reservation]struct{}
	closed   bool
	root     *os.Root
	owner    *os.File
}

func NewPool(config PoolConfig) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if _, err := poolDirectory(config.Directory); err != nil {
		return nil, err
	}
	return &Pool{config: config, slots: admission.New(config.MaxActive), retained: make(map[*Reservation]struct{})}, nil
}

const (
	ownerMarker = ".foundry-upload-owner"
	ownerFormat = "foundry-upload-owner 2\n"
)

func poolDirectory(directory string) (os.FileInfo, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&0022 != 0 {
		return nil, safeerror.NewFault(fault.Invalid, "upload pool requires a private directory without symlink ownership")
	}
	return info, nil
}

// Claim enables reservations for a manually managed lifetime. Repeated calls
// on the same pool are idempotent; they do not transfer ownership.
func (p *Pool) Claim() error { return p.claim(true) }

// ClaimExclusive acquires a new server lifetime or refuses an existing owner,
// including another server or manual Claim on this same pool object.
func (p *Pool) ClaimExclusive() error { return p.claim(false) }

func (p *Pool) claim(idempotent bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return safeerror.NewFault(fault.Closed, "upload pool is closed")
	}
	if p.owner != nil {
		if idempotent {
			return nil
		}
		return safeerror.NewFault(fault.Conflict, "upload pool already has a serving owner; framework servers and manual Claim must not share a pool")
	}
	info, err := poolDirectory(p.config.Directory)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(p.config.Directory)
	if err != nil {
		return safeerror.WrapFailure(fault.Invalid, "upload pool directory is unavailable", err)
	}
	claimed := false
	defer func() {
		if !claimed {
			_ = root.Close()
		}
	}()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return safeerror.NewFault(fault.Conflict, "upload pool directory changed during claim")
	}
	if marker, err := root.Lstat(ownerMarker); err == nil {
		if !marker.Mode().IsRegular() {
			return safeerror.NewFault(fault.Invalid, "invalid upload pool ownership marker")
		}
	} else if !os.IsNotExist(err) {
		return safeerror.WrapFailure(fault.Invalid, "upload pool ownership marker is unavailable", err)
	}
	owner, err := root.OpenFile(ownerMarker, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	created := err == nil
	if os.IsExist(err) {
		owner, err = root.OpenFile(ownerMarker, os.O_RDWR, 0600)
	}
	if err != nil {
		return safeerror.WrapFailure(fault.Invalid, "upload pool ownership marker is unavailable", err)
	}
	defer func() {
		if !claimed {
			_ = owner.Close()
		}
	}()
	identity, err := owner.Stat()
	if err != nil || !identity.Mode().IsRegular() {
		return safeerror.NewFault(fault.Invalid, "invalid upload pool ownership marker")
	}
	locked, err := filelock.Try(owner)
	if err != nil {
		return safeerror.WrapFailure(fault.Invalid, "upload pool ownership lock failed", err)
	}
	if !locked {
		return safeerror.NewFault(fault.Conflict, "upload pool directory has a live owner")
	}
	// Close unlinks while locked. An opener of the old inode must never admit
	// work after a different owner has created and locked the replacement.
	current, err := root.Lstat(ownerMarker)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(identity, current) {
		return safeerror.NewFault(fault.Conflict, "upload pool ownership marker changed during claim; retry startup")
	}
	if created {
		if _, err := owner.WriteString(ownerFormat); err != nil {
			return safeerror.WrapFailure(fault.Internal, "upload pool ownership marker write failed", err)
		}
		if err := owner.Sync(); err != nil {
			return safeerror.WrapFailure(fault.Internal, "upload pool ownership marker sync failed", err)
		}
	} else {
		contents, err := io.ReadAll(io.LimitReader(owner, int64(len(ownerFormat)+1)))
		if err != nil {
			return safeerror.WrapFailure(fault.Invalid, "upload pool ownership marker could not be read", err)
		}
		if len(contents) == 0 {
			return safeerror.NewFault(fault.Conflict, "a v0.30.0 ownership marker was found; stop every v0.30.0 server using this directory, then remove the marker")
		}
		if string(contents) != ownerFormat {
			return safeerror.NewFault(fault.Invalid, "unrecognized upload pool ownership marker format; serving refused without reconciling captures")
		}
	}
	if err := reconcileCaptures(root); err != nil {
		return err
	}
	p.root, p.owner = root, owner
	claimed = true
	return nil
}

// Reservation is held until its owner exits, even after cancellation. Attach
// its batch before capture. Release closes it before returning capacity.
type Reservation struct {
	pool  *Pool
	bytes int64
	mu    sync.Mutex
	batch *Batch
	ended bool
	once  sync.Once
}

func (p *Pool) Reserve(ctx context.Context, bytes int64) (*Reservation, error) {
	if ctx == nil {
		return nil, safeerror.NewFault(fault.Invalid, "upload reservation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.owner == nil {
		return nil, safeerror.NewFault(fault.Closed, "upload pool is not serving")
	}
	if bytes <= 0 || bytes > p.config.MaxBytes {
		return nil, safeerror.NewFault(fault.Invalid, "upload reservation exceeds pool capacity")
	}
	if bytes > p.config.MaxBytes-p.bytes || !p.slots.TryAcquire() {
		return nil, safeerror.NewFault(fault.Overloaded, "upload capacity is exhausted")
	}
	p.bytes += bytes
	return &Reservation{pool: p, bytes: bytes}, nil
}

func (p *Pool) Directory() string { return p.config.Directory }
func (p *Pool) MaxBytes() int64   { return p.config.MaxBytes }

// Usage includes failed cleanup; those bytes cannot be reserved again.
func (p *Pool) Usage() (active int, bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.slots.Active(), p.bytes
}

// Attach binds exactly one bounded capture to the reservation before reading.
func (r *Reservation) Attach(batch *Batch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended || r.batch != nil || batch == nil || batch.config.TempDirectory != r.pool.Directory() || batch.config.MaxBytes > r.bytes {
		return safeerror.NewFault(fault.Invalid, "capture does not fit an unused upload reservation")
	}
	r.batch = batch
	return nil
}

func (r *Reservation) Release() {
	r.once.Do(func() {
		r.mu.Lock()
		r.ended = true
		batch := r.batch
		r.mu.Unlock()
		clean := true
		if batch != nil {
			_ = batch.Close()
			clean = batch.CleanupComplete()
		}
		p := r.pool
		p.mu.Lock()
		defer p.mu.Unlock()
		if clean {
			p.bytes -= r.bytes
		} else {
			p.retained[r] = struct{}{}
		}
		p.slots.Release()
	})
}

// RetryCleanup removes only batch-owned files. A failed batch conservatively
// retains its entire encoded-body reservation, including metadata headroom.
func (p *Pool) RetryCleanup() error {
	p.cleanup.Lock()
	defer p.cleanup.Unlock()
	p.mu.Lock()
	retained := make([]*Reservation, 0, len(p.retained))
	for r := range p.retained {
		retained = append(retained, r)
	}
	p.mu.Unlock()
	var failures []error
	for _, r := range retained {
		failures = append(failures, r.batch.RetryCleanup())
		if r.batch.CleanupComplete() {
			p.mu.Lock()
			p.bytes -= r.bytes
			delete(p.retained, r)
			p.mu.Unlock()
		}
	}
	return safeerror.Join("upload cleanup failed", failures...)
}

// Close seals admission. Active work or failed cleanup retains the marker so
// another process cannot treat occupied storage as a fresh budget.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.slots.Active() != 0 || len(p.retained) != 0 {
		return safeerror.NewFault(fault.Conflict, "upload pool still owns active or retained captures")
	}
	if p.owner == nil {
		return nil
	}
	identity, err := p.owner.Stat()
	if err != nil {
		return safeerror.WrapFailure(fault.Internal, "upload pool ownership inspection failed", err)
	}
	current, err := p.root.Lstat(ownerMarker)
	if err != nil || !os.SameFile(identity, current) {
		return safeerror.NewFault(fault.Conflict, "upload pool ownership marker changed before close")
	}
	if err := p.root.Remove(ownerMarker); err != nil && !os.IsNotExist(err) {
		return safeerror.WrapFailure(fault.Internal, "upload pool ownership cleanup failed", err)
	}
	err = safeerror.Join("upload pool ownership close failed", p.owner.Close(), p.root.Close())
	p.owner, p.root = nil, nil
	return err
}
