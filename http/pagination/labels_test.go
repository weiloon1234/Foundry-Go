package pagination_test

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func issues(t *testing.T, response *httptest.ResponseRecorder) []contract.Issue {
	t.Helper()
	var body foundryhttp.ErrorResponse
	if response.Code != 422 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	return body.Issues
}

// Page parameters name themselves with built-in English labels, which an
// application's catalog translates, and export their label keys.
func TestPaginationParametersCarryTranslatableLabels(t *testing.T) {
	t.Parallel()
	handler := func(context.Context, pageInput) (query.Page[Item], error) {
		t.Error("bad input reached service")
		return query.Page[Item]{}, nil
	}
	endpoint := numbered(pagination.DefaultConfig())
	for target, want := range map[string]contract.Issue{
		"/items?page=0":       {Path: "/query/page", Label: "Page", LabelKey: pagination.NumberLabelKey},
		"/items?per_page=0":   {Path: "/query/per_page", Label: "Page size", LabelKey: pagination.SizeLabelKey},
		"/items?per_page=101": {Path: "/query/per_page", Label: "Page size", LabelKey: pagination.SizeLabelKey},
	} {
		found := issues(t, serve(t, endpoint.Handle(handler), target))
		if len(found) != 1 || found[0].Path != want.Path || found[0].Label != want.Label || found[0].LabelKey != want.LabelKey {
			t.Fatal(target, found)
		}
	}

	// A configured catalog translates the labels in the request's locale.
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, slices.Concat(validation.MessageDefinitions(), foundryhttp.MessageDefinitions(), pagination.MessageDefinitions()),
		map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"ms": {pagination.NumberLabelKey: {Text: "Halaman"}, pagination.SizeLabelKey: {Text: "Saiz halaman"}}})
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(endpoint.WithMiddleware(foundryhttp.Locale(catalog)).Handle(handler))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(stdhttp.MethodGet, "/items?page=0", nil)
	request.Header.Set("Accept-Language", "ms")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if found := issues(t, response); len(found) != 1 || found[0].Label != "Halaman" || found[0].LabelKey != pagination.NumberLabelKey {
		t.Fatal("translated label", found)
	}

	// Exported query metadata carries the same keys.
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]i18n.MessageKey{}
	for _, parameter := range info.Query {
		labels[parameter.Name] = parameter.Presentation.LabelKey
	}
	if labels["page"] != pagination.NumberLabelKey || labels["per_page"] != pagination.SizeLabelKey || labels["q"] != "" {
		t.Fatal("query presentation", labels)
	}
}

func TestCursorPageSizeCarriesItsLabel(t *testing.T) {
	t.Parallel()
	endpoint := cursorEndpoint(pagination.DefaultCursorConfig())
	found := issues(t, serve(t, endpoint.Handle(func(context.Context, cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		t.Error("bad input reached service")
		return pagination.CursorResult[cursorSource, Item]{}, nil
	}), "/items?per_page=0"))
	if len(found) != 1 || found[0].Label != "Page size" || found[0].LabelKey != pagination.SizeLabelKey {
		t.Fatal("cursor size label", found)
	}
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	for _, parameter := range info.Query {
		if want := map[string]i18n.MessageKey{"per_page": pagination.SizeLabelKey}[parameter.Name]; parameter.Presentation.LabelKey != want {
			t.Fatal("cursor query presentation", parameter.Name, parameter.Presentation)
		}
	}
}
