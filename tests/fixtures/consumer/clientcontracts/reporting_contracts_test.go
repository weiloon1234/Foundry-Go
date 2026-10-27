package clientcontracts_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"foundry.test/consumer/internal/clientfixture"
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/value"
)

var reportSurfaces = []typescript.Surface{
	{Name: "reports", Paths: []string{"/reports"}},
	{Name: "exports", Routes: []foundryhttp.RouteID{"reports.members.csv"}},
}

// Use the actual reporting routes and declarations. Anonymous transport checks
// stop at authentication; metadata construction never acquires a SQL connection.
func reportingContracts(t *testing.T) (*manifest.Manifest, *foundryhttp.Router) {
	t.Helper()
	provider := reporting.Operators(func(context.Context, model.ID[reporting.Operator]) (value.Optional[reporting.Operator], error) {
		return value.Optional[reporting.Operator]{}, nil
	})
	strategy := auth.DefineStrategy("report.contract", func(context.Context, secret.String) (value.Optional[auth.Proof[reporting.Operator, model.ID[reporting.Operator]]], error) {
		return value.Optional[auth.Proof[reporting.Operator, model.ID[reporting.Operator]]]{}, nil
	})
	guard := auth.DefineGuard("report.contract", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), reporting.Access.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("report.contract"))
	if err != nil {
		t.Fatal(err)
	}
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := reporting.New(datatable.Dependencies{Database: new(database.DB), Locales: locales, Labels: func(_ context.Context, _ i18n.LocaleID, key i18n.MessageKey) (string, error) { return string(key), nil }}, datatable.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	router, err := foundryhttp.NewRouter(reporting.QueryRoute(manager, transport, guard), reporting.DownloadRoute(manager, transport, guard, datatable.Presentation{Locale: "en", TimeZone: "UTC"}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Tables: manager.Registry()})
	if err != nil {
		t.Fatal(err)
	}
	return source, router
}

// Current outputs publish the native cursor ID. The migration check retains the
// immutable pre-cutover cohort and restricts changes to that ID and ownership hashes.
func TestDatatableContractBytes(t *testing.T) {
	for _, name := range []string{"client", "reporting"} {
		t.Run(name, func(t *testing.T) {
			source, selections := contractBaselineSource(t, name)
			dir := t.TempDir()
			options := typescript.Options{Dir: dir, Prefix: "contracts", OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}, React: true, Vue: true, Surfaces: selections}
			if _, err := typescript.Generate(t.Context(), source, options); err != nil {
				t.Fatal(err)
			}
			options.Check = true
			if _, err := typescript.Generate(t.Context(), source, options); err != nil {
				t.Fatal(err)
			}
			expected := filepath.Join("testdata", "native-cursor-contracts", name)
			assertContractBytes(t, dir, expected)
		})
	}
}

func contractBaselineSource(t *testing.T, name string) (*manifest.Manifest, []typescript.Surface) {
	t.Helper()
	if name == "reporting" {
		source, _ := reportingContracts(t)
		return source, reportSurfaces
	}
	source, err := fixture(t).Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return source, surfaces
}

func assertContractBytes(t *testing.T, actual, baseline string) {
	t.Helper()
	names := func(dir string) []string {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(dir, "*_foundry.gen.*"))
		if err != nil {
			t.Fatal(err)
		}
		result := make([]string, 0, len(files))
		for _, file := range files {
			result = append(result, filepath.Base(file))
		}
		slices.Sort(result)
		return result
	}
	got, want := names(actual), names(baseline)
	if len(want) == 0 || !slices.Equal(got, want) {
		t.Fatalf("contract artifact set changed: got %v, want %v", got, want)
	}
	for _, name := range want {
		got, err := os.ReadFile(filepath.Join(actual, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(baseline, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("published contract bytes changed: %s", name)
		}
	}
}

func TestReportingContractCoverage(t *testing.T) {
	source, router := reportingContracts(t)
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(document.Tables))
	for _, table := range document.Tables {
		ids = append(ids, string(table.ID))
		if !table.Exports {
			t.Fatal("report lost export capability", table.ID)
		}
	}
	if !slices.Equal(ids, []string{"reports.members", "reports.orders", "reports.totals"}) || len(document.HTTP) != 2 {
		t.Fatal("incomplete reporting contract", ids, len(document.HTTP))
	}
	for _, operation := range document.HTTP {
		if operation.Route.Authentication == nil {
			t.Fatal("report route lost authentication", operation.Name)
		}
	}
	for _, request := range []*http.Request{httptest.NewRequest("POST", "/reports/members/query", bytes.NewBufferString(`{}`)), httptest.NewRequest("GET", "/reports/members.csv", nil)} {
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatal("report admitted anonymous request", response.Code)
		}
	}
}

func checkReportingClient(t *testing.T) {
	tools := clientfixture.Load(t)
	source, router := reportingContracts(t)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	tools.Check(t, source, server.URL, filepath.Join("testdata", "reporting"))
}
