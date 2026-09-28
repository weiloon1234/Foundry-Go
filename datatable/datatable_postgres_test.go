package datatable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	spec.Columns[1].declaration.cell = func(_ context.Context, _ ReportRow, p Presentation) (string, error) {
		zone, err := p.Location()
		if err != nil {
			return "", err
		}
		return instant.FormatIn(zone)
	}
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
				if len(sheet.Rows) != 3 || sheet.Rows[0].Cells[0].Text != "ms:reports.id" || sheet.Rows[1].Cells[0].Text != "1" || sheet.Rows[2].Cells[0].Text != "2" {
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
		"cell limit": func(c *Config) { c.MaxCellBytes = 4 },
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
			lease, err := manager.beginExport(t.Context())
			if err != nil {
				t.Fatal("failed export leaked capacity", err)
			}
			lease.Release()
		})
	}
	for _, mode := range []string{"panic", "goexit", "error"} {
		t.Run(mode, func(t *testing.T) {
			spec := reportSpec()
			spec.Columns[1].declaration.cell = func(context.Context, ReportRow, Presentation) (string, error) {
				if mode == "goexit" {
					runtime.Goexit()
				}
				if mode == "panic" {
					panic("private formatter panic")
				}
				return "", errors.New("formatting failed")
			}
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
	spec.Columns[1].declaration.cell = func(ctx context.Context, _ ReportRow, _ Presentation) (string, error) {
		close(entered)
		<-release
		return "", ctx.Err()
	}
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
	if lease, err := manager.beginExport(t.Context()); lease != nil || !errors.Is(err, fault.Conflict) {
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
	spec.Columns[1].declaration.cell = func(ctx context.Context, _ ReportRow, _ Presentation) (string, error) { return "", manager.Close(ctx) }
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
