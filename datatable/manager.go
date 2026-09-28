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
	TimeZone       temporal.ZoneName
	Schema         string
	MaxActive      int
	Timeout        time.Duration
	MaxPageSize    int
	MaxOffset      int
	MaxRowBytes    int
	MaxPageBytes   int
	MaxExports     int
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
		MaxPageSize: query.MaxPageSize, MaxOffset: 1_000_000,
		MaxRowBytes: 256 << 10, MaxPageBytes: 4 << 20,
		MaxExports: 2, ExportTimeout: 10 * time.Minute,
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
	if c.MaxExports < 1 || c.MaxExports > 32 || c.ExportTimeout <= 0 || c.ExportTimeout > time.Hour || c.MaxExportRows < 1 || c.MaxExportRows >= maxWorksheetRows {
		return invalid("invalid datatable export capacity")
	}
	if c.MaxExportBytes < 1 || c.MaxExportBytes > 1<<30 || c.MaxXMLBytes < 1 || c.MaxXMLBytes > 1<<30 || c.MaxCellBytes < 1 || c.MaxCellBytes > 128<<10 || (c.TempDir != "" && !filepath.IsAbs(c.TempDir)) {
		return invalid("invalid datatable configuration")
	}
	return nil
}

// Manager owns bounded query/export lifetimes, but borrows its database and
// locale/label dependencies. Construction performs no I/O and starts no worker.
// Each completed artifact retains export capacity until its caller closes it.
type Manager struct {
	dependencies   Dependencies
	registry       *Registry
	config         Config
	calls          *workscope.Group
	exports        *workscope.Group
	cleanupMu      sync.Mutex
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
	return &Manager{dependencies: dependencies, registry: registry, config: config, calls: calls, exports: exports, pendingCleanup: make(map[*Artifact]struct{})}, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.calls == nil || m.exports == nil || m.dependencies.Database == nil {
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
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_ = m.calls.Close(canceled)
	_ = m.exports.Close(canceled)
	return errors.Join(m.calls.Close(ctx), m.exports.Close(ctx), m.retryCleanup())
}

// beginExport retries previously failed removals before admitting another file.
// Admission and failed-file registration share one lock: an unremovable file
// cannot be replaced with an unbounded sequence of newly admitted exports.
func (m *Manager) beginExport(ctx context.Context) (*workscope.Lease, error) {
	if err := m.retryCleanup(); err != nil {
		return nil, err
	}
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	if len(m.pendingCleanup) != 0 {
		return nil, fault.New(fault.Conflict, "export cleanup is pending")
	}
	return m.exports.Begin(ctx)
}

func (m *Manager) retainCleanup(artifact *Artifact, pending bool) {
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
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
			var err error
			result, err = source.Paginate(ctx, tx, prepared.page)
			return err
		})
		if err != nil {
			return err
		}
		remaining := m.config.MaxPageBytes
		for _, row := range result.Items {
			data, err := t.definition.spec.Row.Encode(ctx, row, rowLimits(min(remaining, m.config.MaxRowBytes)))
			if err != nil {
				return err
			}
			remaining -= len(data)
		}
		return ctx.Err()
	})
	if err != nil {
		return query.Page[R]{}, err
	}
	return result, nil
}

// Count applies the exact Query authorization, server scope and client filters.
// Request pagination is validated but does not restrict the unpaginated total.
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
		return m.read(ctx, func(tx *database.Tx) error { var err error; result, err = source.Count(ctx, tx); return err })
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
