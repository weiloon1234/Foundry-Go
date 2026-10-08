package reporting_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

func assertNoExportFiles(t *testing.T, f *fixture) {
	t.Helper()
	files, err := os.ReadDir(f.tempDir)
	if err != nil || len(files) != 0 {
		t.Fatal("HTTP response retained an export artifact", len(files), err)
	}
}

func TestPublicReportHTTPAuthorizationRangesAndArtifactOwnership(t *testing.T) {
	f := openFixture(t)
	transport, err := foundryhttp.NewAuthentication(f.registry, foundryhttp.BearerCredential("report.test"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(reporting.DownloadRoute(f.manager, transport, f.guard, datatable.Presentation{Locale: "en", TimeZone: "UTC"}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/reports/members.csv", nil)
	request.Header.Set("Authorization", "Bearer report-fixture")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.Contains(response.Header().Get("Content-Disposition"), "reports.members.csv") {
		t.Fatal("typed report download failed", response.Code, response.Body.String())
	}
	rows, err := csv.NewReader(bytes.NewReader(response.Body.Bytes())).ReadAll()
	if err != nil || len(rows) != 4 || rows[1][1] != "'=SUM(A1:A2)" || rows[1][4] != "9007199254740993.125" {
		t.Fatal("HTTP export lost scoped exact/safe text", rows, err)
	}
	assertNoExportFiles(t, f)
	rangeRequest := httptest.NewRequest("GET", "/reports/members.csv", nil)
	rangeRequest.Header.Set("Authorization", "Bearer report-fixture")
	rangeRequest.Header.Set("Range", "bytes=0-15")
	// The content validator lets a resume prove it continues the same bytes.
	tag := response.Header().Get("ETag")
	if !strings.HasPrefix(tag, `"sha256-`) || response.Header().Get("Last-Modified") == "" {
		t.Fatal("report download lacks content validators", response.Header())
	}
	rangeRequest.Header.Set("If-Range", tag)
	ranged := httptest.NewRecorder()
	router.ServeHTTP(ranged, rangeRequest)
	if ranged.Code != 206 || !bytes.Equal(ranged.Body.Bytes(), response.Body.Bytes()[:16]) {
		t.Fatal("report did not reuse seekable HTTP range transport", ranged.Code)
	}
	assertNoExportFiles(t, f)
	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest("GET", "/reports/members.csv", nil))
	if anonymous.Code != 401 {
		t.Fatal("anonymous export was admitted", anonymous.Code)
	}
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_operators SET can_export=false`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deniedRequest := httptest.NewRequest("GET", "/reports/members.csv", nil)
	deniedRequest.Header.Set("Authorization", "Bearer report-fixture")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, deniedRequest)
	if denied.Code != 403 || strings.Contains(denied.Body.String(), "SUM(A1:A2)") {
		t.Fatal("fresh export policy was not applied before response headers", denied.Code)
	}
	assertNoExportFiles(t, f)
}

func TestPublicReportDownloadOwnsFrozenFilterTree(t *testing.T) {
	f := openFixture(t)
	transport, err := foundryhttp.NewAuthentication(f.registry, foundryhttp.BearerCredential("report.test"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.frozen", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/frozen")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv; charset=utf-8"))
	route := foundryhttp.RequireAuthentication(endpoint, transport, f.guard).Handle(func(ctx context.Context, _ reporting.Operator, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		input := datatable.Request{Filters: []datatable.Filter{{Op: datatable.All, Children: []datatable.Filter{{Op: datatable.Equal, Column: "name", Values: []string{"Ada"}}}}}}
		download, err := reporting.Members.Download(ctx, f.manager, reporting.FromGuard(f.guard), input, datatable.ExportOptions{Format: datatable.CSV})
		input.Filters[0].Children[0].Values[0] = "Foreign"
		return download, err
	})
	router, err := foundryhttp.NewRouter(route)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/frozen", nil)
	request.Header.Set("Authorization", "Bearer report-fixture")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	rows, err := csv.NewReader(bytes.NewReader(response.Body.Bytes())).ReadAll()
	if err != nil || response.Code != 200 || len(rows) != 3 || rows[1][0] != f.members[0].String() || rows[2][0] != f.members[1].String() {
		t.Fatal("deferred download reused caller-owned filter buffers", response.Code, rows, err)
	}
	assertNoExportFiles(t, f)
}

func TestPublicReportQueryReturnsRequestErrorsUnchanged(t *testing.T) {
	f := openFixture(t)
	transport, err := foundryhttp.NewAuthentication(f.registry, foundryhttp.BearerCredential("report.test"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(reporting.QueryRoute(f.manager, transport, f.guard))
	if err != nil {
		t.Fatal(err)
	}
	post := func(body string, authenticated bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/reports/members/query", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if authenticated {
			request.Header.Set("Authorization", "Bearer report-fixture")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, body := range []string{
		`{"page":-1}`, `{"size":1001}`,
		`{"sort":[{"column":"private-canary","direction":"asc"}]}`,
		`{"filters":[{"column":"state","op":"eq","values":["private-canary"]}]}`,
	} {
		response := post(body, true)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"error_code":"bad_request"`) || strings.Contains(response.Body.String(), "private-canary") {
			t.Fatal("unmodified table error did not produce safe HTTP 400", response.Code, response.Body.String())
		}
	}
	if response := post(`{}`, false); response.Code != 401 {
		t.Fatal("anonymous query admitted", response.Code)
	}
	response := post(`{}`, true)
	var page pagination.NumberedResponse[reporting.MemberRow]
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || response.Code != 200 || page.Meta.Total != 3 || len(page.Data) != 3 {
		t.Fatal("healthy scoped query failed after rejection", response.Code, err)
	}
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE report_operators SET can_view=false`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if response := post(`{}`, true); response.Code != 403 {
		t.Fatal("fresh policy failure became bad request", response.Code)
	}
	assertNoExportFiles(t, f)
}
