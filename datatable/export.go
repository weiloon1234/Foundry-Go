package datatable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

//foundry:enum
type ExportFormat string

const (
	CSV  ExportFormat = "csv"
	XLSX ExportFormat = "xlsx"
)

//foundry:dto
type ExportOptions struct {
	Format       ExportFormat `json:"format"`
	Name         string       `json:"name,omitempty"`
	Presentation Presentation `json:"presentation"`
}

// Artifact owns one complete private file. Close is mandatory and idempotent;
// it closes the file and releases its operation lease. Failed removals remain
// owned by the manager and block new exports until cleanup succeeds. Close,
// the next export and manager shutdown retry removal. The path is not public.
// Cancellation/shutdown invalidates reads but does not abandon actual ownership.
// Reads, seeks and close are serialized; callers must not copy an Artifact.
type Artifact struct {
	mu              sync.Mutex
	file            *os.File
	path            string
	lease           *workscope.Lease
	manager         *Manager
	name            string
	media           foundryhttp.MediaType
	size            int64
	rows            int
	digest          string
	closed, removed bool
	closeErr        error
}

func (a *Artifact) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}
func (a *Artifact) MediaType() foundryhttp.MediaType {
	if a == nil {
		return ""
	}
	return a.media
}
func (a *Artifact) Size() int64 {
	if a == nil {
		return 0
	}
	return a.size
}
func (a *Artifact) Rows() int {
	if a == nil {
		return 0
	}
	return a.rows
}
func (a *Artifact) SHA256() string {
	if a == nil {
		return ""
	}
	return a.digest
}
func (a *Artifact) Read(p []byte) (int, error) {
	if a == nil {
		return 0, invalid("export artifact is not initialized")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.file == nil || a.lease == nil {
		return 0, fault.New(fault.Closed, "export artifact is closed")
	}
	if err := a.lease.Context().Err(); err != nil {
		return 0, err
	}
	n, err := a.file.Read(p)
	if canceled := a.lease.Context().Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
func (a *Artifact) Seek(offset int64, whence int) (int64, error) {
	if a == nil {
		return 0, invalid("export artifact is not initialized")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.file == nil || a.lease == nil {
		return 0, fault.New(fault.Closed, "export artifact is closed")
	}
	if err := a.lease.Context().Err(); err != nil {
		return 0, err
	}
	return a.file.Seek(offset, whence)
}
func (a *Artifact) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.closed {
		a.closed = true
		if a.file != nil {
			a.closeErr = a.file.Close()
		}
	}
	if !a.removed && a.path != "" {
		err := os.Remove(a.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			if a.manager != nil {
				a.manager.retainCleanup(a, true)
				a.lease.Release()
			}
			return errors.Join(a.closeErr, fault.Wrap(fault.Internal, "export file cleanup failed", err))
		}
		a.removed = true
	}
	if a.manager != nil {
		a.manager.retainCleanup(a, false)
	}
	a.lease.Release()
	return a.closeErr
}

// Export applies the same validated request and server scope as Query. Page and
// Size are validated, but all matching rows are exported up to MaxExportRows.
// No artifact is returned until the database stream and complete CSV/ZIP finish.
// Locale defaults to the configured default; an empty time zone means UTC.
func (t Table[S, R, A]) Export(ctx context.Context, m *Manager, subject A, request Request, options ExportOptions) (result *Artifact, err error) {
	if err := t.check(m); err != nil {
		return nil, err
	}
	if !t.definition.spec.Exports {
		return nil, invalid("table does not permit exports")
	}
	if options.Format != CSV && options.Format != XLSX || len(options.Name) > filename.MaxBytes {
		return nil, invalid("invalid datatable export options")
	}
	lease, err := m.beginExport(ctx)
	if err != nil {
		return nil, err
	}
	var artifact *Artifact
	defer func() {
		if result == nil {
			if artifact != nil {
				err = errors.Join(err, artifact.Close())
			} else {
				lease.Release()
			}
		}
	}()
	ctx = lease.Context()
	err = callback.Isolated("datatable export", func() error {
		prepared, err := t.definition.prepare(request, m.config)
		if err != nil {
			return err
		}
		// Authorization occurs before locale/label callbacks, files or database work.
		source, err := t.definition.scoped(ctx, subject, ExportAction, prepared)
		if err != nil {
			return err
		}
		locales, err := i18n.SnapshotLocales(ctx, m.dependencies.Locales)
		if err != nil {
			return err
		}
		presentation := options.Presentation
		if presentation.Locale == "" {
			presentation.Locale = locales.Default()
		}
		if presentation.Locale.Validate() != nil || !locales.Contains(presentation.Locale) {
			return invalid("export locale is not supported")
		}
		if presentation.TimeZone == "" {
			presentation.TimeZone = string(m.config.TimeZone)
		}
		if len(presentation.TimeZone) > maxTimeZoneBytes {
			return invalid("invalid export time zone")
		}
		if _, err := presentation.Location(); err != nil {
			return err
		}
		var columns []columnDeclaration[S, R]
		var headings []string
		for _, column := range t.definition.columns {
			if column.cell == nil {
				continue
			}
			text, err := m.dependencies.Labels(ctx, presentation.Locale, column.label)
			if err != nil {
				return err
			}
			if err := validateCell(text, m.config.MaxCellBytes); err != nil {
				return err
			}
			columns = append(columns, column)
			headings = append(headings, text)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.CreateTemp(m.config.TempDir, "foundry-report-*")
		if err != nil {
			return fault.Wrap(fault.Internal, "create export file failed", err)
		}
		artifact = &Artifact{file: file, path: file.Name(), lease: lease, manager: m, name: exportName(options.Name, t.ID(), options.Format), media: exportMedia(options.Format)}
		digest := sha256.New()
		bounded := &exportWriter{ctx: ctx, file: file, digest: digest, maximum: m.config.MaxExportBytes}
		writer, err := newReportWriter(bounded, options.Format, m.config, headings)
		if err != nil {
			return err
		}
		defer writer.Abort()
		err = m.read(ctx, func(tx *database.Tx) error {
			return source.Limit(m.config.MaxExportRows+1).Each(ctx, tx, func(row R) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if artifact.rows >= m.config.MaxExportRows {
					return invalid("export row limit exceeded")
				}
				if _, err := t.definition.spec.Row.Encode(ctx, row, rowLimits(m.config.MaxRowBytes)); err != nil {
					return err
				}
				cells := make([]string, len(columns))
				for i, column := range columns {
					text, err := column.cell(ctx, row, presentation)
					if err != nil {
						return err
					}
					if err := validateCell(text, m.config.MaxCellBytes); err != nil {
						return err
					}
					cells[i] = text
				}
				if err := writer.Row(cells); err != nil {
					return err
				}
				artifact.rows++
				return nil
			})
		})
		if err != nil {
			return err
		}
		if err := writer.Finish(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		artifact.size = bounded.written
		artifact.digest = hex.EncodeToString(digest.Sum(nil))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return artifact, nil
}

type exportWriter struct {
	ctx              context.Context
	file             io.Writer
	digest           hash.Hash
	maximum, written int64
}

func (w *exportWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.maximum-w.written {
		return 0, invalid("export byte limit exceeded")
	}
	n, err := w.file.Write(p)
	w.written += int64(n)
	if n > 0 {
		_, _ = w.digest.Write(p[:n])
	}
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
func exportName(name string, id TableID, format ExportFormat) string {
	if name == "" {
		name = string(id)
	}
	name = filename.Normalize(name, "report")
	if extension := filename.Extension(name); extension == "csv" || extension == "xlsx" {
		name = name[:strings.LastIndexByte(name, '.')]
	}
	return filename.Normalize(filename.Truncate(name, filename.MaxBytes-5)+"."+string(format), "report."+string(format))
}
func exportMedia(format ExportFormat) foundryhttp.MediaType {
	if format == CSV {
		return "text/csv; charset=utf-8"
	}
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}
