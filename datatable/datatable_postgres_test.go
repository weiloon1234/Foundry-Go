package datatable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func reportPostgres(t *testing.T) extensiontest.Fixture {
	t.Helper()
	fixture := extensiontest.Open(t, nil)
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, statement := range []string{
			`CREATE TABLE report_rows (id bigint PRIMARY KEY, tenant_id bigint NOT NULL, name text NOT NULL, note text, amount numeric NOT NULL, deleted_at timestamptz)`,
			`INSERT INTO report_rows VALUES (1,7,'Alpha',NULL,1.25,NULL),(2,7,'Alpha','memo',2.50,NULL),(3,7,'=SUM(A1:A2)','x,y',9007199254740993.125,NULL),(4,7,'Hidden',NULL,99,now()),(5,8,'Foreign',NULL,99,NULL),(6,7,'beta','Alpha',4,NULL)`,
		} {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestPostgresExportUsesConfiguredTimeZoneAndExplicitOverride(t *testing.T) {
	fixture := reportPostgres(t)
	instant, err := temporal.ParseDateTime("2026-09-25T17:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	spec := reportSpec()
	spec.Columns[1].declaration.cell = textCellFunc(func(_ context.Context, _ ReportRow, p Presentation) (string, error) {
		zone, err := p.Location()
		if err != nil {
			return "", err
		}
		return instant.FormatIn(zone)
	})
	manager, table := reportManager(t, fixture, spec, func(c *Config) { c.TimeZone = "Asia/Kuala_Lumpur" })
	for _, tc := range []struct{ override, want string }{
		{"", "2026-09-26T01:30:00+08:00"}, {"UTC", "2026-09-25T17:30:00Z"},
	} {
		artifact, err := table.Export(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV, Presentation: Presentation{TimeZone: tc.override}})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(artifact)
		closeErr := artifact.Close()
		if err != nil || closeErr != nil || !strings.Contains(string(data), tc.want) {
			t.Fatal("export zone not applied", err, closeErr)
		}
	}
	noReportFiles(t, manager)
}

func reportManager(t *testing.T, fixture extensiontest.Fixture, spec Spec[reportRecord, ReportRow, reportActor], configure func(*Config)) (*Manager, Table[reportRecord, ReportRow, reportActor]) {
	t.Helper()
	config := DefaultConfig()
	config.Schema = fixture.Schema
	config.TempDir = t.TempDir()
	if configure != nil {
		configure(&config)
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	table := Define(spec)
	manager, err := New(Dependencies{Database: fixture.DB, Locales: locales, Labels: func(_ context.Context, locale i18n.LocaleID, key i18n.MessageKey) (string, error) {
		if locale == "ms" {
			return "ms:" + string(key), nil
		}
		return strings.TrimPrefix(string(key), "reports."), nil
	}}, config, table.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager, table
}

// textCellFunc replaces a column formatter in tests with custom text output.
func textCellFunc(format func(context.Context, ReportRow, Presentation) (string, error)) func(context.Context, ReportRow, Presentation) (exportCell, error) {
	return func(ctx context.Context, row ReportRow, p Presentation) (exportCell, error) {
		text, err := format(ctx, row, p)
		return exportCell{text: text}, err
	}
}

// assertExportCapacity proves no retention or generation slot leaked.
func assertExportCapacity(t *testing.T, manager *Manager) {
	t.Helper()
	if active := manager.retained.Active(); active != 0 {
		t.Fatal("export leaked retention capacity", active)
	}
	lease, err := manager.exports.Begin(t.Context())
	if err != nil {
		t.Fatal("export leaked generation capacity", err)
	}
	lease.Release()
}

func noReportFiles(t *testing.T, manager *Manager) {
	t.Helper()
	files, err := os.ReadDir(manager.config.TempDir)
	if err != nil || len(files) != 0 {
		t.Fatal("export left incomplete files", len(files), err)
	}
}

func TestPostgresDatatableScopesPaginationAndQueryCount(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	request := Request{Page: 1, Size: 1, Search: "ALPHA"}
	before := fixture.Queries.Load()
	first, err := table.Query(t.Context(), manager, actor, request)
	if err != nil || first.Total != 2 || first.Pages != 2 || len(first.Items) != 1 || first.Items[0].ID != 1 {
		t.Fatal("scoped first page failed", first, err)
	}
	if got := fixture.Queries.Load() - before; got != 2 {
		t.Fatal("page did not use one count and one row query", got)
	}
	request.Page = 2
	second, err := table.Query(t.Context(), manager, actor, request)
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].ID != 2 {
		t.Fatal("duplicate sort keys destabilized pagination", second, err)
	}
	request.Page = 3
	last, err := table.Query(t.Context(), manager, actor, request)
	if err != nil || last.Total != 2 || len(last.Items) != 0 {
		t.Fatal("beyond-last page lost the unpaginated total", last, err)
	}
	before = fixture.Queries.Load()
	count, err := table.Count(t.Context(), manager, actor, Request{})
	if err != nil || count != 4 || fixture.Queries.Load()-before != 1 {
		t.Fatal("count leaked other tenants, deleted rows or used extra queries", count, err)
	}
	nullRows, err := table.Query(t.Context(), manager, actor, Request{Filters: []Filter{{Op: IsNull, Column: "note"}}})
	if err != nil || nullRows.Total != 1 || nullRows.Items[0].ID != 1 {
		t.Fatal("nullable filter changed semantics", err)
	}
	amountRows, err := table.Query(t.Context(), manager, actor, Request{Filters: []Filter{{Op: Between, Column: "amount", Values: []string{"1.250", "2.500"}}}, Sort: []Sort{{Column: "amount", Direction: Descending}}})
	if err != nil || amountRows.Total != 2 || amountRows.Items[0].ID != 2 || amountRows.Items[1].ID != 1 {
		t.Fatal("exact decimal filter or order failed", err)
	}
	foreign, err := table.Query(t.Context(), manager, reportActor{Tenant: 8, Allowed: true}, Request{})
	if err != nil || foreign.Total != 1 || foreign.Items[0].ID != 5 {
		t.Fatal("server scope was not reevaluated for the actor", err)
	}
	before = fixture.Queries.Load()
	if result, err := table.Query(t.Context(), manager, reportActor{Tenant: 7}, Request{}); !errors.Is(err, auth.Forbidden) || !reflect.DeepEqual(result, query.Page[ReportRow]{}) {
		t.Fatal("unauthorized query returned a page", err)
	}
	if _, err := table.Count(t.Context(), manager, actor, Request{Sort: []Sort{{Column: "id;DROP TABLE report_rows", Direction: Ascending}}}); err == nil {
		t.Fatal("malicious sort accepted")
	}
	if _, err := table.Query(t.Context(), manager, actor, Request{Filters: []Filter{{Op: Equal, Column: "id", Values: []string{"not an integer"}}}}); err == nil {
		t.Fatal("invalid scalar accepted")
	}
	if fixture.Queries.Load() != before {
		t.Fatal("rejected request reached SQL")
	}
}

func TestPostgresDatatableCSVAndXLSXUseSameScopeAndCompleteArtifacts(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	for _, format := range []ExportFormat{CSV, XLSX} {
		t.Run(string(format), func(t *testing.T) {
			before := fixture.Queries.Load()
			artifact, err := table.Export(t.Context(), manager, actor, Request{Page: 2, Size: 1, Search: "alpha"}, ExportOptions{Format: format, Name: "../people.csv", Presentation: Presentation{Locale: "ms", TimeZone: "Asia/Kuala_Lumpur"}})
			if err != nil {
				t.Fatal(err)
			}
			defer artifact.Close()
			if artifact.Rows() != 2 || artifact.Name() != "people."+string(format) || artifact.Size() <= 0 {
				t.Fatal("export used page window or unsafe filename")
			}
			if fixture.Queries.Load()-before != 1 {
				t.Fatal("export did not stream a single query")
			}
			data, err := io.ReadAll(artifact)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			if int64(len(data)) != artifact.Size() || hex.EncodeToString(digest[:]) != artifact.SHA256() {
				t.Fatal("artifact completion metadata does not match delivered bytes")
			}
			if format == CSV {
				rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
				if err != nil || len(rows) != 3 || rows[0][0] != "ms:reports.id" || rows[1][0] != "1" || rows[2][0] != "2" {
					t.Fatal("CSV scope, locale or stable order changed", rows, err)
				}
			} else {
				_, sheet := readWorkbook(t, data)
				if len(sheet.Rows) != 3 || sheet.Rows[0].Cells[0].Text != "ms:reports.id" || sheet.Rows[1].Cells[0].Value != "1" || sheet.Rows[2].Cells[0].Value != "2" {
					t.Fatal("XLSX scope, locale or stable order changed")
				}
			}
			if err := artifact.Close(); err != nil {
				t.Fatal(err)
			}
			noReportFiles(t, manager)
		})
	}
	before := fixture.Queries.Load()
	if artifact, err := table.Export(t.Context(), manager, reportActor{}, Request{}, ExportOptions{Format: CSV}); !errors.Is(err, auth.Forbidden) || artifact != nil {
		t.Fatal("unauthorized export produced an artifact", err)
	}
	if fixture.Queries.Load() != before {
		t.Fatal("unauthorized export reached SQL")
	}
	noReportFiles(t, manager)
}

func TestPostgresDatatableExportFailuresRemovePartialFiles(t *testing.T) {
	fixture := reportPostgres(t)
	for name, configure := range map[string]func(*Config){
		"row limit":  func(c *Config) { c.MaxExportRows = 1 },
		"file limit": func(c *Config) { c.MaxExportBytes = 8 },
		"XML limit":  func(c *Config) { c.MaxXMLBytes = 100 },
		"row bytes":  func(c *Config) { c.MaxRowBytes = 8; c.MaxPageBytes = 8 },
	} {
		t.Run(name, func(t *testing.T) {
			manager, table := reportManager(t, fixture, reportSpec(), configure)
			format := CSV
			if name == "XML limit" {
				format = XLSX
			}
			if artifact, err := table.Export(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: format}); err == nil || artifact != nil {
				t.Fatal("limited export published an artifact", err)
			}
			noReportFiles(t, manager)
			assertExportCapacity(t, manager)
		})
	}
	for _, mode := range []string{"panic", "goexit", "error"} {
		t.Run(mode, func(t *testing.T) {
			spec := reportSpec()
			spec.Columns[1].declaration.cell = textCellFunc(func(context.Context, ReportRow, Presentation) (string, error) {
				if mode == "goexit" {
					runtime.Goexit()
				}
				if mode == "panic" {
					panic("private formatter panic")
				}
				return "", errors.New("formatting failed")
			})
			manager, table := reportManager(t, fixture, spec, nil)
			if artifact, err := table.Export(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: XLSX}); err == nil || artifact != nil {
				t.Fatal("failed formatter published an artifact", err)
			}
			noReportFiles(t, manager)
			if count, err := table.Count(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}); err != nil || count != 4 {
				t.Fatal("failed export leaked its transaction or borrowed pool", err)
			}
		})
	}
	manager, table := reportManager(t, fixture, reportSpec(), func(c *Config) { c.MaxRowBytes = 64; c.MaxPageBytes = 64 })
	if page, err := table.Query(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}); err == nil || !reflect.DeepEqual(page, query.Page[ReportRow]{}) {
		t.Fatal("page byte limit leaked a partial result", err)
	}
}

func TestPostgresDatatableCancellationRetainsCallbackOwnership(t *testing.T) {
	fixture := reportPostgres(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseAll := func() { once.Do(func() { close(release) }) }
	spec := reportSpec()
	spec.Columns[1].declaration.cell = textCellFunc(func(ctx context.Context, _ ReportRow, _ Presentation) (string, error) {
		close(entered)
		<-release
		return "", ctx.Err()
	})
	manager, table := reportManager(t, fixture, spec, func(c *Config) { c.MaxExports = 1 })
	t.Cleanup(releaseAll)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		artifact, err := table.Export(ctx, manager, reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV})
		if artifact != nil {
			_ = artifact.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("formatter did not begin")
	}
	cancel()
	wait, stopWaiting := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stopWaiting()
	if lease, err := manager.exports.Begin(wait); lease != nil || !errors.Is(err, fault.Overloaded) {
		t.Fatal("cancellation abandoned executing formatter", err)
	}
	select {
	case <-done:
		t.Fatal("export returned before callback exited")
	default:
	}
	releaseAll()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled export did not exit")
	}
	noReportFiles(t, manager)
	if count, err := table.Count(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}); err != nil || count != 4 {
		t.Fatal("cancellation leaked database resources", err)
	}
}

func TestPostgresDatatableSelfShutdownDoesNotCancelEitherGroup(t *testing.T) {
	fixture := reportPostgres(t)
	var manager *Manager
	spec := reportSpec()
	spec.Columns[1].declaration.cell = textCellFunc(func(ctx context.Context, _ ReportRow, _ Presentation) (string, error) { return "", manager.Close(ctx) })
	created, table := reportManager(t, fixture, spec, nil)
	manager = created
	if artifact, err := table.Export(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV}); !errors.Is(err, fault.Cycle) || artifact != nil {
		t.Fatal("self-shutdown did not reject dependency cycle", err)
	}
	if count, err := table.Count(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}); err != nil || count != 4 {
		t.Fatal("self-shutdown partially canceled query group", err)
	}
	noReportFiles(t, manager)
}

func TestPostgresNegatedFiltersIncludeNullRows(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	memo := Filter{Op: Equal, Column: "note", Values: []string{"memo"}}
	for name, tc := range map[string]struct {
		filter Filter
		want   int64
	}{
		"ne":            {Filter{Op: NotEqual, Column: "note", Values: []string{"memo"}}, 3},
		"not_in":        {Filter{Op: NotIn, Column: "note", Values: []string{"memo", "x,y"}}, 2},
		"not eq":        {Filter{Op: Not, Children: []Filter{memo}}, 3},
		"not is_null":   {Filter{Op: Not, Children: []Filter{{Op: IsNull, Column: "note"}}}, 3},
		"not or":        {Filter{Op: Not, Children: []Filter{{Op: Any, Children: []Filter{memo, {Op: Contains, Column: "note", Values: []string{"x"}}}}}}, 2},
		"not and":       {Filter{Op: Not, Children: []Filter{{Op: All, Children: []Filter{memo, {Op: Equal, Column: "id", Values: []string{"2"}}}}}}, 3},
		"not between":   {Filter{Op: Not, Children: []Filter{{Op: Between, Column: "amount", Values: []string{"1", "3"}}}}, 2},
		"non-null ne":   {Filter{Op: NotEqual, Column: "name", Values: []string{"Alpha"}}, 2},
		"double not":    {Filter{Op: Not, Children: []Filter{{Op: Not, Children: []Filter{memo}}}}, 1},
		"not not_in":    {Filter{Op: Not, Children: []Filter{{Op: NotIn, Column: "note", Values: []string{"memo"}}}}, 1},
		"not ne (null)": {Filter{Op: Not, Children: []Filter{{Op: NotEqual, Column: "note", Values: []string{"memo"}}}}, 1},
	} {
		count, err := table.Count(t.Context(), manager, actor, Request{Filters: []Filter{tc.filter}})
		if err != nil || count != tc.want {
			t.Fatal("negated filter changed NULL semantics", name, count, err)
		}
	}
}

func TestPostgresSimpleQueryReadsWithoutCounting(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	before := fixture.Queries.Load()
	byID := []Sort{{Column: "id", Direction: Ascending}}
	first, err := table.SimpleQuery(t.Context(), manager, actor, Request{Size: 3, Sort: byID})
	if err != nil || !first.HasMore || len(first.Items) != 3 || first.Items[0].ID != 1 || first.Items[2].ID != 3 || fixture.Queries.Load()-before != 1 {
		t.Fatal("simple page did not use one lookahead query", first, err)
	}
	second, err := table.SimpleQuery(t.Context(), manager, actor, Request{Page: 2, Size: 3, Sort: byID})
	if err != nil || second.HasMore || len(second.Items) != 1 || second.Items[0].ID != 6 {
		t.Fatal("last simple page changed", second, err)
	}
	if page, err := table.SimpleQuery(t.Context(), manager, reportActor{Tenant: 7}, Request{}); !errors.Is(err, auth.Forbidden) || len(page.Items) != 0 {
		t.Fatal("unauthorized simple query returned rows", err)
	}
}

func TestPostgresExportSlotsReleaseWhenArtifactsComplete(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), func(c *Config) { c.MaxExports = 1; c.MaxArtifacts = 2 })
	actor := reportActor{Tenant: 7, Allowed: true}
	first, err := table.Export(t.Context(), manager, actor, Request{}, ExportOptions{Format: CSV})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// The open first artifact no longer holds the only generation slot.
	second, err := table.Export(t.Context(), manager, actor, Request{}, ExportOptions{Format: CSV})
	if err != nil {
		t.Fatal("open artifact blocked export generation", err)
	}
	defer second.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if artifact, err := table.Export(ctx, manager, actor, Request{}, ExportOptions{Format: CSV}); artifact != nil || !errors.Is(err, fault.Overloaded) {
		t.Fatal("retained artifacts exceeded their bound", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := table.Export(t.Context(), manager, actor, Request{}, ExportOptions{Format: CSV})
	if err != nil {
		t.Fatal("closed artifact did not release retention", err)
	}
	if err := errors.Join(second.Close(), third.Close()); err != nil {
		t.Fatal(err)
	}
	noReportFiles(t, manager)
	assertExportCapacity(t, manager)
}

func TestPostgresExportWritesUnsafeTextAndTypedSpreadsheetCells(t *testing.T) {
	fixture := reportPostgres(t)
	spec := reportSpec()
	long := "bad\x00\x1bchar" + strings.Repeat("a", 70_000)
	spec.Columns[2].declaration.cell = textCellFunc(func(context.Context, ReportRow, Presentation) (string, error) { return long, nil })
	manager, table := reportManager(t, fixture, spec, nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	request := Request{Filters: []Filter{{Op: Equal, Column: "id", Values: []string{"3"}}}}
	artifact, err := table.Export(t.Context(), manager, actor, request, ExportOptions{Format: CSV, ByteOrderMark: true})
	if err != nil {
		t.Fatal("one unsafe cell failed the CSV export", err)
	}
	data, err := io.ReadAll(artifact)
	if closeErr := artifact.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	if !bytes.HasPrefix(data, []byte("\uFEFF")) {
		t.Fatal("CSV byte-order mark missing")
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\uFEFF")))).ReadAll()
	if err != nil || len(rows) != 2 || !strings.HasPrefix(rows[1][2], "bad\x00\x1bchar") || !strings.HasSuffix(rows[1][2], truncationMarker) || len(rows[1][2]) > DefaultConfig().MaxCellBytes {
		t.Fatal("CSV did not carry controls or truncate at the byte bound", err)
	}
	artifact, err = table.Export(t.Context(), manager, actor, request, ExportOptions{Format: XLSX})
	if err != nil {
		t.Fatal("one unsafe cell failed the XLSX export", err)
	}
	data, err = io.ReadAll(artifact)
	if closeErr := artifact.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	_, sheet := readWorkbook(t, data)
	cells := sheet.Rows[1].Cells
	note := decodeSpreadsheetText(cells[2].Text)
	if cells[0].Value != "3" || cells[0].Style != "1" || cells[3].Type != "inlineStr" || cells[3].Text != "9007199254740993.125" {
		t.Fatal("XLSX typed cells lost exactness", cells[0], cells[3])
	}
	if !strings.HasPrefix(note, "bad\x00\x1bchar") || !strings.HasSuffix(note, truncationMarker) || len(utf16.Encode([]rune(note))) > maxCellUTF16Units {
		t.Fatal("XLSX did not escape controls or truncate the spreadsheet cell")
	}
	if artifact, err := table.Export(t.Context(), manager, actor, request, ExportOptions{Format: XLSX, ByteOrderMark: true}); err == nil || artifact != nil {
		t.Fatal("XLSX accepted a byte-order mark option")
	}
	noReportFiles(t, manager)
}

func TestPostgresDownloadValidatorsPreventSplicedRangeResumes(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.download", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/report.csv")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv; charset=utf-8"))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return table.Download(ctx, manager, actor, Request{}, ExportOptions{Format: CSV})
	}))
	if err != nil {
		t.Fatal(err)
	}
	get := func(headers map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/report.csv", nil)
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	full := get(nil)
	digest := sha256.Sum256(full.Body.Bytes())
	tag := `"sha256-` + hex.EncodeToString(digest[:]) + `"`
	if full.Code != 200 || full.Header().Get("ETag") != tag || full.Header().Get("Last-Modified") == "" {
		t.Fatal("download lacks content validators", full.Code, full.Header())
	}
	if resumed := get(map[string]string{"Range": "bytes=0-9", "If-Range": tag}); resumed.Code != 206 || !bytes.Equal(resumed.Body.Bytes(), full.Body.Bytes()[:10]) {
		t.Fatal("identical report did not resume", resumed.Code)
	}
	if unchanged := get(map[string]string{"If-None-Match": tag}); unchanged.Code != 304 {
		t.Fatal("unchanged report was not validated", unchanged.Code)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE report_rows SET name = 'Changed' WHERE id = 6`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	changed := get(map[string]string{"Range": "bytes=0-9", "If-Range": tag})
	if changed.Code != 200 || !strings.Contains(changed.Body.String(), "Changed") || changed.Header().Get("ETag") == tag {
		t.Fatal("range resume spliced a different export run", changed.Code)
	}
	noReportFiles(t, manager)
}

// serveReports runs routes through the real HTTP server and kernel with the
// supplied kernel RequestTimeout and returns the server's base URL.
func serveReports(t *testing.T, requestTimeout time.Duration, routes ...foundryhttp.RouteRegistration) string {
	t.Helper()
	router, err := foundryhttp.NewRouter(routes...)
	if err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.RequestTimeout = requestTimeout
	server, err := foundryhttp.Prepare(router, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("report server did not stop")
		}
	})
	ready, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	return "http://" + address
}

// waitForExportRelease proves that a failed or abandoned export promptly gave
// back its retention and generation slots, which it holds together with its
// read transaction, and removed its temporary file.
func waitForExportRelease(t *testing.T, manager *Manager, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		files, err := os.ReadDir(manager.config.TempDir)
		if err != nil {
			t.Fatal(err)
		}
		if manager.retained.Active() == 0 && len(files) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("export kept its slot or file after the request ended", manager.retained.Active(), len(files))
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), within)
	defer cancel()
	lease, err := manager.exports.Begin(ctx)
	if err != nil {
		t.Fatal("export generation slot was not released", err)
	}
	lease.Release()
}

// A slow download succeeds under its declared route deadline, fails with the
// kernel deadline otherwise, and a disconnected client stops generation.
func TestPostgresDownloadRunsUnderItsRouteDeadlineAndReleasesOnCancel(t *testing.T) {
	fixture := reportPostgres(t)
	var blocking atomic.Bool
	started := make(chan struct{}, 1)
	spec := reportSpec()
	spec.Columns[1].declaration.cell = textCellFunc(func(ctx context.Context, row ReportRow, _ Presentation) (string, error) {
		if blocking.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return "", ctx.Err()
		}
		// Four rows take longer than the kernel deadline in total.
		select {
		case <-time.After(100 * time.Millisecond):
			return row.Name, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	manager, table := reportManager(t, fixture, spec, func(c *Config) { c.MaxExports, c.MaxArtifacts = 1, 1 })
	if manager.DownloadTimeout() != 2*manager.config.ExportTimeout || DefaultConfig().DownloadTimeout() != 20*time.Minute {
		t.Fatal("download deadline is not derived from the export timeout")
	}
	actor := reportActor{Tenant: 7, Allowed: true}
	download := func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return table.Download(ctx, manager, actor, Request{}, ExportOptions{Format: CSV})
	}
	kernel := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.kernel", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/kernel.csv")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv; charset=utf-8"))
	routed := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.routed", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/routed.csv")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv; charset=utf-8")).WithTimeout(manager.DownloadTimeout())
	base := serveReports(t, 150*time.Millisecond, kernel.Handle(download), routed.Handle(download))
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 10 * time.Second}
	get := func(ctx context.Context, path string) (*stdhttp.Response, []byte, error) {
		request, err := stdhttp.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return nil, nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		return response, body, err
	}

	response, _, err := get(t.Context(), "/kernel.csv")
	if err != nil || response.StatusCode != 503 {
		t.Fatal("slow export ignored the kernel deadline", err)
	}
	waitForExportRelease(t, manager, 2*time.Second)

	response, body, err := get(t.Context(), "/routed.csv")
	if err != nil || response.StatusCode != 200 {
		t.Fatal("slow export failed within its route deadline", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil || len(rows) != 5 || !slices.ContainsFunc(rows, func(row []string) bool { return row[1] == "Alpha" }) {
		t.Fatal("route-deadline export lost rows", rows, err)
	}
	waitForExportRelease(t, manager, 2*time.Second)

	blocking.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, _, err := get(ctx, "/routed.csv"); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("export did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("client cancellation was not observed", err)
	}
	// The route deadline is 20 minutes: only the disconnect can end this export.
	waitForExportRelease(t, manager, 2*time.Second)
}

// Exports read committed rows only, in their own read-only snapshot: a row a
// concurrent transaction has not committed never reaches an artifact or its
// delivery, and appears once committed.
func TestPostgresExportsNeverIncludeUncommittedRows(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	names := func() []string {
		t.Helper()
		artifact, err := table.Export(t.Context(), manager, actor, Request{}, ExportOptions{Format: CSV})
		if err != nil {
			t.Fatal(err)
		}
		defer artifact.Close()
		rows, err := csv.NewReader(artifact).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		var result []string
		for _, row := range rows[1:] {
			result = append(result, row[1])
		}
		return result
	}
	inserted, release := make(chan struct{}), make(chan struct{})
	writer := make(chan error, 1)
	go func() {
		writer <- fixture.Store.Write(context.Background(), func(ctx context.Context, tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO report_rows VALUES (9,7,'Pending',NULL,1,NULL)`); err != nil {
				return err
			}
			close(inserted)
			<-release
			return errors.New("roll back the pending row")
		})
	}()
	select {
	case <-inserted:
	case err := <-writer:
		t.Fatal("pending writer failed", err)
	}
	if got := names(); slices.Contains(got, "Pending") || len(got) != 4 {
		t.Fatal("export included an uncommitted row", got)
	}
	close(release)
	if err := <-writer; err == nil {
		t.Fatal("pending writer committed")
	}
	if got := names(); slices.Contains(got, "Pending") {
		t.Fatal("export included a rolled-back row", got)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO report_rows VALUES (9,7,'Committed',NULL,1,NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Contains(got, "Committed") {
		t.Fatal("export missed a committed row", got)
	}
	noReportFiles(t, manager)
}
