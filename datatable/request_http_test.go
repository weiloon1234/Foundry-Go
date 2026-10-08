package datatable

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func assertTableHTTPFailure(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected table failure")
	}
	response := httptest.NewRecorder()
	if failure := foundryhttp.WriteError(response, httptest.NewRequest("GET", "/report", nil), err); failure != nil {
		t.Fatal(failure)
	}
	if response.Code != status || strings.Contains(response.Body.String(), "private-canary") {
		t.Fatal("wrong or unsafe table failure response", response.Code, response.Body.String())
	}
	if errors.Is(err, foundryhttp.BadRequest) != (status == 400) {
		t.Fatal("Go callers cannot distinguish request rejection", err)
	}
}

func TestTableRequestFailuresHaveDistinctHTTPClassification(t *testing.T) {
	table := Define(reportSpec())
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{Page: -1}, {Size: -1}, {Page: 10002, Size: 1},
		{Sort: []Sort{{Column: "private-canary", Direction: Ascending}}},
		{Filters: []Filter{{Op: Equal, Column: "private-canary", Values: []string{"private-canary"}}}},
		{Filters: []Filter{{Op: Equal, Column: "id", Values: []string{"private-canary"}}}},
		{Filters: []Filter{{Op: Between, Column: "id", Values: []string{"1"}}}},
		{Search: strings.Repeat("private-canary", MaxScalarBytes)},
	} {
		_, err := table.definition.prepare(request, DefaultConfig())
		assertTableHTTPFailure(t, err, 400)
		if !errors.Is(err, fault.Invalid) {
			t.Fatal("lost fault compatibility", err)
		}
	}
	for _, input := range []string{`{"unknown":"private-canary"}`, `{"page":"private-canary"}`, `{`} {
		_, err := DecodeRequest(t.Context(), []byte(input))
		assertTableHTTPFailure(t, err, 400)
		var rejected *contract.DecodeError
		if !errors.As(err, &rejected) || !errors.Is(err, fault.Invalid) {
			t.Fatal("lost decoder diagnostics", err)
		}
	}
}

func TestTableConfigurationAndProgrammingFaultsStayInternal(t *testing.T) {
	spec := reportSpec()
	spec.DefaultSort = []Sort{{Column: "private-canary", Direction: Ascending}}
	assertTableHTTPFailure(t, Define(spec).Validate(), 500)
	config := DefaultConfig()
	config.MaxPageSize = -1
	assertTableHTTPFailure(t, config.Validate(), 500)
	_, err := DecodeRequest(nil, []byte(`{}`))
	assertTableHTTPFailure(t, err, 500)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = DecodeRequest(ctx, []byte(`{}`))
	if !errors.Is(err, context.Canceled) || errors.Is(err, foundryhttp.BadRequest) {
		t.Fatal("cancellation became client rejection", err)
	}
}

func TestPostgresTableOperationsPreserveRequestAndServerBoundaries(t *testing.T) {
	fixture := reportPostgres(t)
	actor := reportActor{Tenant: 7, Allowed: true}
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	calls := []struct {
		name string
		run  func(Table[reportRecord, ReportRow, reportActor], *Manager, reportActor, Request) error
	}{
		{"query", func(table Table[reportRecord, ReportRow, reportActor], m *Manager, a reportActor, r Request) error {
			_, err := table.Query(t.Context(), m, a, r)
			return err
		}},
		{"simple", func(table Table[reportRecord, ReportRow, reportActor], m *Manager, a reportActor, r Request) error {
			_, err := table.SimpleQuery(t.Context(), m, a, r)
			return err
		}},
		{"count", func(table Table[reportRecord, ReportRow, reportActor], m *Manager, a reportActor, r Request) error {
			_, err := table.Count(t.Context(), m, a, r)
			return err
		}},
		{"export", func(table Table[reportRecord, ReportRow, reportActor], m *Manager, a reportActor, r Request) error {
			artifact, err := table.Export(t.Context(), m, a, r, ExportOptions{Format: CSV})
			if artifact != nil {
				t.Fatal("failed export returned artifact")
			}
			return err
		}},
		{"download", func(table Table[reportRecord, ReportRow, reportActor], m *Manager, a reportActor, r Request) error {
			_, err := table.Download(t.Context(), m, a, r, ExportOptions{Format: CSV})
			return err
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			err := call.run(table, manager, actor, Request{Sort: []Sort{{Column: "private-canary", Direction: Ascending}}})
			assertTableHTTPFailure(t, err, 400)
		})
	}
	for _, options := range []ExportOptions{
		{Format: "private-canary"}, {Format: XLSX, ByteOrderMark: true},
		{Format: CSV, Presentation: Presentation{Locale: "zz"}},
		{Format: CSV, Presentation: Presentation{TimeZone: "private-canary"}},
	} {
		artifact, err := table.Export(t.Context(), manager, actor, Request{}, options)
		if artifact != nil {
			t.Fatal("rejected options returned artifact")
		}
		assertTableHTTPFailure(t, err, 400)
	}
	for _, call := range calls[:4] {
		err := call.run(table, manager, reportActor{Tenant: 7}, Request{})
		assertTableHTTPFailure(t, err, 403)
	}
	spec := reportSpec()
	spec.Source = func(context.Context, reportActor) (query.ProjectionQuery[reportRecord, ReportRow], error) {
		return query.ProjectionQuery[reportRecord, ReportRow]{}, fault.New(fault.Invalid, "private-canary source bug")
	}
	brokenManager, brokenTable := reportManager(t, fixture, spec, nil)
	for _, call := range calls[:4] {
		assertTableHTTPFailure(t, call.run(brokenTable, brokenManager, actor, Request{}), 500)
	}
	page, err := table.Query(t.Context(), manager, actor, Request{})
	if err != nil || page.Total != 4 {
		t.Fatal("healthy query failed after rejections", err)
	}
	assertExportCapacity(t, manager)
	noReportFiles(t, manager)
}
