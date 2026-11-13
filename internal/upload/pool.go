package upload

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// PoolConfig reserves capture slots and bytes independently of downstream work.
// Directory must be an empty, private, writable directory on a quota-controlled
// volume. A retained ownership marker refuses reuse after an unclean shutdown.
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
	marker   string
}

func NewPool(config PoolConfig) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(config.Directory)
	if err != nil || !info.IsDir() || info.Mode()&0022 != 0 {
		return nil, safeerror.NewFault(fault.Invalid, "upload pool requires a private directory without symlink ownership")
	}
	entries, err := os.ReadDir(config.Directory)
	if err != nil {
		return nil, safeerror.WrapFailure(fault.Invalid, "upload pool directory is unavailable", err)
	}
	if len(entries) != 0 {
		return nil, safeerror.NewFault(fault.Invalid, "upload pool directory contains retained or active ownership; reconcile it before reuse")
	}
	marker := filepath.Join(config.Directory, ".foundry-upload-owner")
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, safeerror.WrapFailure(fault.Invalid, "upload pool directory is not exclusively writable", err)
	}
	if err := file.Close(); err != nil {
		return nil, safeerror.WrapFailure(fault.Internal, "upload pool ownership close failed", err)
	}
	return &Pool{config: config, slots: admission.New(config.MaxActive), retained: make(map[*Reservation]struct{}), marker: marker}, nil
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
	if p.closed {
		return nil, safeerror.NewFault(fault.Closed, "upload pool is closed")
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
	if p.marker == "" {
		return nil
	}
	if err := os.Remove(p.marker); err != nil && !os.IsNotExist(err) {
		return safeerror.WrapFailure(fault.Internal, "upload pool ownership cleanup failed", err)
	}
	p.marker = ""
	return nil
}
