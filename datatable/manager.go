package datatable

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Presentation is explicit, serializable export context, suitable for an
// ordinary typed job payload. It never consults process-global request state.
//
//foundry:dto
type Presentation struct {
	Locale   i18n.LocaleID `json:"locale"`
	TimeZone string        `json:"time_zone"`
}

const maxTimeZoneBytes = 128

func (p Presentation) Location() (*time.Location, error) { return temporal.ParseTimeZone(p.TimeZone) }

// Labels resolves UI message keys for export headings. Catalogs are borrowed;
// it must be concurrency-safe, honor cancellation and return bounded text.
type Labels func(context.Context, i18n.LocaleID, i18n.MessageKey) (string, error)
type Dependencies struct {
	Database *database.DB
	Locales  i18n.LocaleCatalog
	Labels   Labels
}
type Config struct {
	// TimeZone is the default export presentation zone. Empty means UTC for a
	// direct manager, or inherits the application timezone during assembly.
	TimeZone     temporal.ZoneName
	Schema       string
	MaxActive    int
	Timeout      time.Duration
	MaxPageSize  int
	MaxOffset    int
	MaxRowBytes  int
	MaxPageBytes int
	// MaxExports bounds concurrent export generation (query, formatting and
	// file writing). A slot is released once the artifact is complete.
	MaxExports int
	// MaxArtifacts bounds completed artifacts still open for delivery, and so
	// the private temporary files held on disk. It is at least MaxExports.
	MaxArtifacts   int
	ExportTimeout  time.Duration
	MaxExportRows  int
	MaxExportBytes int64
	MaxXMLBytes    int64
	MaxCellBytes   int
	TempDir        string
}

func DefaultConfig() Config {
	return Config{
		Schema: "public", MaxActive: 32, Timeout: time.Minute,
		MaxPageSize: query.MaxPageSize, MaxOffset: 10_000,
		MaxRowBytes: 256 << 10, MaxPageBytes: 4 << 20,
		MaxExports: 2, MaxArtifacts: 16, ExportTimeout: 10 * time.Minute,
		MaxExportRows: 50_000, MaxExportBytes: 64 << 20,
		MaxXMLBytes: 256 << 20, MaxCellBytes: 64 << 10,
	}
}
func (c Config) Validate() error {
	if len(c.TimeZone) > maxTimeZoneBytes {
		return invalid("invalid export timezone")
	}
	if c.TimeZone != "" {
		if _, err := c.TimeZone.Location(); err != nil {
			return err
		}
	}
	if !sqlname.Valid(c.Schema) || c.MaxActive < 1 || c.MaxActive > 1024 || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return invalid("invalid datatable service configuration")
	}
	if c.MaxPageSize < DefaultPageSize || c.MaxPageSize > query.MaxPageSize || c.MaxOffset < 0 || c.MaxOffset > 10_000_000 || c.MaxRowBytes < 1 || c.MaxRowBytes > 4<<20 || c.MaxPageBytes < c.MaxRowBytes || c.MaxPageBytes > 64<<20 {
		return invalid("invalid datatable page configuration")
	}
	if c.MaxExports < 1 || c.MaxExports > 32 || c.MaxArtifacts < c.MaxExports || c.MaxArtifacts > 1024 || c.ExportTimeout <= 0 || c.ExportTimeout > time.Hour || c.MaxExportRows < 1 || c.MaxExportRows >= maxWorksheetRows {
		return invalid("invalid datatable export capacity")
	}
	if c.MaxExportBytes < 1 || c.MaxExportBytes > 1<<30 || c.MaxXMLBytes < 1 || c.MaxXMLBytes > 1<<30 || c.MaxCellBytes < minCellBytes || c.MaxCellBytes > 128<<10 || (c.TempDir != "" && !filepath.IsAbs(c.TempDir)) {
		return invalid("invalid datatable configuration")
	}
	return nil
}

// Manager owns bounded query/export lifetimes, but borrows its database and
// locale/label dependencies. Construction performs no I/O and starts no worker.
// Export generation holds an export slot until its artifact is complete; the
// artifact then holds only a retention slot and its private file until Close.
type Manager struct {
	dependencies   Dependencies
	registry       *Registry
	config         Config
	calls          *workscope.Group
	exports        *workscope.Group
	retained       *admission.Semaphore
	stop           context.Context
	halt           context.CancelFunc
	cleanupMu      sync.Mutex
	closing        bool
	open           map[*Artifact]struct{}
	pendingCleanup map[*Artifact]struct{}
}

func New(dependencies Dependencies, config Config, registrations ...Registration) (*Manager, error) {
	if config.TimeZone == "" {
		config.TimeZone = temporal.UTC
	}
	if dependencies.Database == nil {
		return nil, invalid("datatables require a database")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	registry, err := NewRegistry(registrations...)
	if err != nil {
		return nil, err
	}
	for _, item := range registry.entries {
		info, err := item.describe()
		if err != nil {
			return nil, err
		}
		if info.Exports && (dependencies.Labels == nil || i18n.ValidateLocaleCatalog(dependencies.Locales) != nil) {
			return nil, invalid("exports require a locale catalog and label resolver")
		}
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	exports, err := workscope.New(config.MaxExports, config.ExportTimeout)
	if err != nil {
		return nil, err
	}
	stop, halt := context.WithCancel(context.Background())
	return &Manager{dependencies: dependencies, registry: registry, config: config, calls: calls, exports: exports, retained: admission.New(config.MaxArtifacts), stop: stop, halt: halt, open: make(map[*Artifact]struct{}), pendingCleanup: make(map[*Artifact]struct{})}, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.calls == nil || m.exports == nil || m.retained == nil || m.dependencies.Database == nil {
		return invalid("datatable manager is not initialized")
	}
	return m.registry.Validate()
}
func (m *Manager) Registry() *Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// Close stops admission, cancels active queries and export generation, and
// closes every completed artifact that is still open: their readers observe
// fault.Closed and their private files are removed. Completed artifacts use no
// borrowed dependency, so shutdown never waits for their owners. The context
// bounds only the caller's wait for callbacks that are still running.
func (m *Manager) Close(ctx context.Context) error {
	if err := m.Validate(); err != nil {
		return err
	}
	// Start cancellation in both groups before waiting for either. Already
	// canceled contexts still close admission; their errors are collected below.
	if ctx == nil {
		return invalid("datatable shutdown requires a context")
	}
	if err := m.calls.CheckClose(ctx); err != nil {
		return err
	}
	if err := m.exports.CheckClose(ctx); err != nil {
		return err
	}
	if m.delivering(ctx) {
		return fault.New(fault.Cycle, "export delivery cannot wait for its own manager")
	}
	m.cleanupMu.Lock()
	m.closing = true
	open := make([]*Artifact, 0, len(m.open))
	for artifact := range m.open {
		open = append(open, artifact)
	}
	m.cleanupMu.Unlock()
	m.halt()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_ = m.calls.Close(canceled)
	_ = m.exports.Close(canceled)
	for _, artifact := range open {
		// A failed removal stays owned in pendingCleanup and is reported below.
		_ = artifact.Close()
	}
	return errors.Join(m.calls.Close(ctx), m.exports.Close(ctx), m.retryCleanup())
}

// beginArtifact retries previously failed removals, then waits (bounded) for a
// retention slot before any export work starts. Admission and failed-file
// registration share one lock: an unremovable file cannot be replaced with an
// unbounded sequence of newly admitted exports.
func (m *Manager) beginArtifact(ctx context.Context) (func(), error) {
	if err := m.retryCleanup(); err != nil {
		return nil, err
	}
	if err := m.admissible(); err != nil {
		return nil, err
	}
	if err := m.retained.Acquire(ctx, admission.Wait(m.config.ExportTimeout), m.stop.Done()); err != nil {
		return nil, err
	}
	if err := m.admissible(); err != nil {
		m.retained.Release()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(m.retained.Release) }, nil
}

func (m *Manager) admissible() error {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	if m.closing {
		return fault.New(fault.Closed, "datatable manager is closed")
	}
	if len(m.pendingCleanup) != 0 {
		return fault.New(fault.Conflict, "export cleanup is pending")
	}
	return nil
}

// publish records a completed artifact so shutdown can close it. It fails when
// shutdown began after generation finished but before publication.
func (m *Manager) publish(artifact *Artifact) error {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	if m.closing {
		return fault.New(fault.Closed, "datatable manager is closed")
	}
	m.open[artifact] = struct{}{}
	return nil
}

func (m *Manager) retainCleanup(artifact *Artifact, pending bool) {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	delete(m.open, artifact)
	if pending {
		m.pendingCleanup[artifact] = struct{}{}
	} else {
		delete(m.pendingCleanup, artifact)
	}
}

func (m *Manager) retryCleanup() error {
	m.cleanupMu.Lock()
	pending := make([]*Artifact, 0, len(m.pendingCleanup))
	for artifact := range m.pendingCleanup {
		pending = append(pending, artifact)
	}
	m.cleanupMu.Unlock()
	var err error
	for _, artifact := range pending {
		err = errors.Join(err, artifact.Close())
	}
	return err
}

// DoneQueries and DoneExports expose actual resource exit separately, without
// a joining background goroutine. Module shutdown waits for both.
func (m *Manager) DoneQueries() <-chan struct{} {
	if m == nil {
		var g *workscope.Group
		return g.Done()
	}
	return m.calls.Done()
}
func (m *Manager) DoneExports() <-chan struct{} {
	if m == nil {
		var g *workscope.Group
		return g.Done()
	}
	return m.exports.Done()
}
func (t Table[S, R, A]) check(m *Manager) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	registration, ok := m.registry.entries[t.ID()]
	if !ok || registration.identity != t.definition.identity {
		return fault.New(fault.Missing, "exact datatable declaration is not registered")
	}
	return nil
}
func (m *Manager) read(ctx context.Context, fn func(*database.Tx) error) error {
	return m.dependencies.Database.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+m.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.RepeatableRead, ReadOnly: true})
}

// Query returns one numbered page and the complete matching total: one count
// query without the table's ordering and one ordered row query, in a single
// read-only repeatable-read transaction.
func (t Table[S, R, A]) Query(ctx context.Context, m *Manager, subject A, request Request) (query.Page[R], error) {
	if err := t.check(m); err != nil {
		return query.Page[R]{}, err
	}
	var result query.Page[R]
	err := m.calls.Run(ctx, "datatable query", func(ctx context.Context) error {
		prepared, err := t.definition.prepare(request, m.config)
		if err != nil {
			return err
		}
		source, err := t.definition.scoped(ctx, subject, QueryAction, prepared)
		if err != nil {
			return err
		}
		err = m.read(ctx, func(tx *database.Tx) error {
			total, err := source.count.Count(ctx, tx)
			if err != nil {
				return err
			}
			page := prepared.page
			items, err := source.rows.Limit(page.Size).Offset((page.Number-1)*page.Size).All(ctx, tx)
			if err != nil {
				return err
			}
			result = query.Page[R]{Items: items, Number: page.Number, Size: page.Size, Total: total, Pages: (total + int64(page.Size) - 1) / int64(page.Size)}
			return result.Validate()
		})
		if err != nil {
			return err
		}
		return t.checkPageBytes(ctx, m, result.Items)
	})
	if err != nil {
		return query.Page[R]{}, err
	}
	return result, nil
}

// SimpleQuery returns one page without counting: the row query reads one
// lookahead row to report HasMore. Use it for large or expensive sources where
// a total is not needed. Validation, authorization and scope match Query.
func (t Table[S, R, A]) SimpleQuery(ctx context.Context, m *Manager, subject A, request Request) (query.SimplePage[R], error) {
	if err := t.check(m); err != nil {
		return query.SimplePage[R]{}, err
	}
	var result query.SimplePage[R]
	err := m.calls.Run(ctx, "datatable simple query", func(ctx context.Context) error {
		prepared, err := t.definition.prepare(request, m.config)
		if err != nil {
			return err
		}
		source, err := t.definition.scoped(ctx, subject, QueryAction, prepared)
		if err != nil {
			return err
		}
		err = m.read(ctx, func(tx *database.Tx) error {
			var err error
			result, err = source.rows.SimplePaginate(ctx, tx, prepared.page)
			return err
		})
		if err != nil {
			return err
		}
		return t.checkPageBytes(ctx, m, result.Items)
	})
	if err != nil {
		return query.SimplePage[R]{}, err
	}
	return result, nil
}

// checkPageBytes applies the per-row and per-page encoded response bounds.
func (t Table[S, R, A]) checkPageBytes(ctx context.Context, m *Manager, rows []R) error {
	remaining := m.config.MaxPageBytes
	for _, row := range rows {
		data, err := t.definition.spec.Row.Encode(ctx, row, rowLimits(min(remaining, m.config.MaxRowBytes)))
		if err != nil {
			return err
		}
		remaining -= len(data)
	}
	return ctx.Err()
}

// Count applies the exact Query authorization, server scope and client filters.
// Request pagination is validated but does not restrict the unpaginated total.
// The count query omits the table's ordering.
func (t Table[S, R, A]) Count(ctx context.Context, m *Manager, subject A, request Request) (int64, error) {
	if err := t.check(m); err != nil {
		return 0, err
	}
	var result int64
	err := m.calls.Run(ctx, "datatable count", func(ctx context.Context) error {
		prepared, err := t.definition.prepare(request, m.config)
		if err != nil {
			return err
		}
		source, err := t.definition.scoped(ctx, subject, QueryAction, prepared)
		if err != nil {
			return err
		}
		return m.read(ctx, func(tx *database.Tx) error { var err error; result, err = source.count.Count(ctx, tx); return err })
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}
func (t Table[S, R, A]) Inspect(ctx context.Context, m *Manager, subject A) (Description, error) {
	if err := t.check(m); err != nil {
		return Description{}, err
	}
	var result Description
	err := m.calls.Run(ctx, "datatable inspection", func(ctx context.Context) error {
		if err := t.definition.spec.Authorize(ctx, subject, InspectAction); err != nil {
			return err
		}
		var err error
		result, err = t.Description()
		if err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return Description{}, err
	}
	return result, nil
}
func rowLimits(bytes int) contract.JSONLimits {
	return contract.JSONLimits{Bytes: bytes, Depth: 32, Nodes: 16384, Steps: 32768, Issues: 16}
}
