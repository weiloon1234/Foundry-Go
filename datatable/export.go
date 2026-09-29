package datatable

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
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
	// ByteOrderMark prefixes CSV with a UTF-8 byte-order mark so spreadsheet
	// programs that guess legacy encodings open non-ASCII text correctly. It
	// is rejected for XLSX, whose XML declares its encoding.
	ByteOrderMark bool `json:"byte_order_mark,omitempty"`
}

// Artifact owns one complete private file. Close is mandatory and idempotent;
// it closes and removes the file and releases its retention slot. Export
// generation capacity was already released when the file was completed, so a
// slow download or delivery never blocks other exports from generating. Failed
// removals remain owned by the manager and block new exports until cleanup
// succeeds; Close, the next export and manager shutdown retry removal. The
// path is not public. Manager shutdown closes artifacts that are still open.
// Reads, seeks and close are serialized; callers must not copy an Artifact.
type Artifact struct {
	mu              sync.Mutex
	file            *os.File
	path            string
	manager         *Manager
	release         func()
	deadline        time.Time
	name            string
	media           foundryhttp.MediaType
	size            int64
	rows            int
	digest          string
	modified        time.Time
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

// Modified is the UTC time the artifact was completed.
func (a *Artifact) Modified() time.Time {
	if a == nil {
		return time.Time{}
	}
	return a.modified
}

// EntityTag is the strong HTTP validator of the exact artifact bytes. Each
// download regenerates the report, so If-Range/If-None-Match compare content:
// a range resume against a different run receives the complete new file.
func (a *Artifact) EntityTag() foundryhttp.EntityTag {
	if a == nil || a.digest == "" {
		return ""
	}
	return foundryhttp.EntityTag(`"sha256-` + a.digest + `"`)
}
func (a *Artifact) Read(p []byte) (int, error) {
	if a == nil {
		return 0, invalid("export artifact is not initialized")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.file == nil {
		return 0, fault.New(fault.Closed, "export artifact is closed")
	}
	return a.file.Read(p)
}
func (a *Artifact) Seek(offset int64, whence int) (int64, error) {
	if a == nil {
		return 0, invalid("export artifact is not initialized")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.file == nil {
		return 0, fault.New(fault.Closed, "export artifact is closed")
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
			}
			a.releaseSlot()
			return errors.Join(a.closeErr, fault.Wrap(fault.Internal, "export file cleanup failed", err))
		}
		a.removed = true
	}
	if a.manager != nil {
		a.manager.retainCleanup(a, false)
	}
	a.releaseSlot()
	return a.closeErr
}
func (a *Artifact) releaseSlot() {
	if a.release != nil {
		a.release()
	}
}

// Export applies the same validated request and server scope as Query. Page and
// Size are validated, but all matching rows are exported up to MaxExportRows.
// No artifact is returned until the database stream and complete CSV/ZIP finish.
// Locale defaults to the configured default; an empty time zone means the
// configured export zone. A cell that is not valid UTF-8, contains control
// characters or exceeds its bound is written safely instead of failing.
func (t Table[S, R, A]) Export(ctx context.Context, m *Manager, subject A, request Request, options ExportOptions) (result *Artifact, err error) {
	if err := t.check(m); err != nil {
		return nil, err
	}
	if !t.definition.spec.Exports {
		return nil, invalid("table does not permit exports")
	}
	if options.Format != CSV && options.Format != XLSX || len(options.Name) > filename.MaxBytes || options.ByteOrderMark && options.Format != CSV {
		return nil, invalid("invalid datatable export options")
	}
	if ctx == nil {
		return nil, invalid("datatable export requires a context")
	}
	release, err := m.beginArtifact(ctx)
	if err != nil {
		return nil, err
	}
	lease, err := m.exports.Begin(ctx)
	if err != nil {
		release()
		return nil, err
	}
	// Generation capacity ends once the file is complete or removed; the
	// artifact keeps only its retention slot while its owner delivers it.
	defer lease.Release()
	artifact := &Artifact{manager: m, release: release, name: exportName(options.Name, t.ID(), options.Format), media: exportMedia(options.Format)}
	defer func() {
		if result == nil {
			err = errors.Join(err, artifact.Close())
		}
	}()
	ctx = lease.Context()
	deadline, _ := ctx.Deadline()
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
		location, err := presentation.Location()
		if err != nil {
			return err
		}
		units := math.MaxInt
		if options.Format == XLSX {
			units = maxCellUTF16Units
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
			columns = append(columns, column)
			headings = append(headings, exportText(text, m.config.MaxCellBytes, units))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.CreateTemp(m.config.TempDir, "foundry-report-*")
		if err != nil {
			return fault.Wrap(fault.Internal, "create export file failed", err)
		}
		artifact.file, artifact.path = file, file.Name()
		digest := sha256.New()
		bounded := &exportWriter{ctx: ctx, file: file, digest: digest, maximum: m.config.MaxExportBytes}
		buffered := bufio.NewWriterSize(bounded, exportBufferBytes)
		writer, err := newReportWriter(buffered, reportLayout{format: options.Format, byteOrderMark: options.ByteOrderMark, location: location, maxXMLBytes: m.config.MaxXMLBytes}, headings)
		if err != nil {
			return err
		}
		defer writer.Abort()
		cells := make([]exportCell, len(columns))
		err = m.read(ctx, func(tx *database.Tx) error {
			return source.rows.Limit(m.config.MaxExportRows+1).Each(ctx, tx, func(row R) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if artifact.rows >= m.config.MaxExportRows {
					return invalid("export row limit exceeded")
				}
				rowBytes := 0
				for i, column := range columns {
					cell, err := column.cell(ctx, row, presentation)
					if err != nil {
						return err
					}
					cell.text = exportText(cell.text, m.config.MaxCellBytes, units)
					if rowBytes += len(cell.text); rowBytes > m.config.MaxRowBytes {
						return invalid("export row exceeds its byte bound")
					}
					cells[i] = cell
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
		if err := buffered.Flush(); err != nil {
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
		artifact.modified = time.Now().UTC()
		return nil
	})
	if err != nil {
		return nil, err
	}
	artifact.deadline = deadline
	if err := m.publish(artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}

// exportBufferBytes batches small CSV/ZIP writes into fewer file writes.
const exportBufferBytes = 64 << 10

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

type deliveryKey struct{}

// deliveryFrame marks a context that is delivering an artifact of manager.
// Closing that manager from inside its own delivery is a dependency cycle.
type deliveryFrame struct {
	manager *Manager
	parent  *deliveryFrame
	active  atomic.Bool
}

func (m *Manager) delivering(ctx context.Context) bool {
	for f, _ := ctx.Value(deliveryKey{}).(*deliveryFrame); f != nil; f = f.parent {
		if f.manager == m && f.active.Load() {
			return true
		}
	}
	return false
}

// deliveryContext keeps the caller's values and cancellation, applies the
// export's deadline and ends when the manager shuts down.
func (m *Manager) deliveryContext(ctx context.Context, artifact *Artifact) (context.Context, func()) {
	ctx, cancel := context.WithDeadline(ctx, artifact.deadline)
	ctx, unlink := contextlink.Link(ctx, m.stop)
	parent, _ := ctx.Value(deliveryKey{}).(*deliveryFrame)
	frame := &deliveryFrame{manager: m, parent: parent}
	frame.active.Store(true)
	return context.WithValue(ctx, deliveryKey{}, frame), func() {
		frame.active.Store(false)
		unlink()
		cancel()
	}
}
