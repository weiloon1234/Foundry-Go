package pagination_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Item struct {
	Label string `json:"label"`
}
type filters struct{ Term value.Optional[string] }
type pageInput = pagination.Request[foundryhttp.NoPath, filters]

func itemJSON() contract.JSON[Item] {
	typ := reflect.TypeFor[Item]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[Item](contract.Schema{Root: id, Types: []contract.Type{
		{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "label", Type: "string", Required: true}}},
		{ID: "string", Kind: contract.StringKind},
	}})
}
func route() foundryhttp.Route[foundryhttp.NoPath] {
	return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "items.index", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/items"))
}
func filterQuery() foundryhttp.Query[filters] {
	return foundryhttp.DefineQuery(foundryhttp.OptionalQueryParam("q", foundryhttp.StringQuery[string](), func(f *filters) *value.Optional[string] { return &f.Term }))
}
func numbered(config pagination.Config) pagination.NumberedEndpoint[foundryhttp.NoPath, filters, Item] {
	return pagination.DefineNumbered(route(), filterQuery(), itemJSON(), config)
}
func serve(t *testing.T, registration foundryhttp.RouteRegistration, target string) *httptest.ResponseRecorder {
	t.Helper()
	router, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
	return response
}
func decodeNumbered(t *testing.T, response *httptest.ResponseRecorder) pagination.NumberedResponse[Item] {
	t.Helper()
	var result pagination.NumberedResponse[Item]
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	// The exported contract must validate the actual bytes from native HTTP.
	if _, err := pagination.NumberedJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestNumberedDefaultsScopedLinksAndEmptyPages(t *testing.T) {
	t.Parallel()
	endpoint := numbered(pagination.DefaultConfig()).Within(foundryhttp.DefineScope("/api", "api"))
	got := decodeNumbered(t, serve(t, endpoint.Handle(func(_ context.Context, in pageInput) (query.Page[Item], error) {
		if in.Page != (query.PageRequest{Number: 1, Size: 20}) {
			t.Error("default ORM request lost")
		}
		if text, _ := in.Filters.Term.Get(); text != "a+b / &" {
			t.Error("typed filter lost")
		}
		return query.Page[Item]{Items: []Item{{Label: "getter value"}}, Number: in.Page.Number, Size: in.Page.Size, Total: 21, Pages: 2}, nil
	}), "http://untrusted.example/api/items?q=a%2Bb+%2F+%26"))
	next, ok := got.Links.Next.Get()
	if !ok || !got.Links.Previous.IsNull() {
		t.Fatal("navigation directions lost")
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.IsAbs() || parsed.Path != "/api/items" || parsed.Query().Get("q") != "a+b / &" || parsed.Query().Get("page") != "2" || parsed.Query().Get("per_page") != "20" {
		t.Fatalf("next=%q err=%v", next, err)
	}
	if got.Data[0].Label != "getter value" || got.Meta.Total != 21 || got.Meta.Pages != 2 {
		t.Fatal("DTO/metadata changed")
	}
	for _, number := range []int{1, 5} {
		got = decodeNumbered(t, serve(t, endpoint.Handle(func(_ context.Context, in pageInput) (query.Page[Item], error) {
			return query.Page[Item]{Number: in.Page.Number, Size: in.Page.Size}, nil
		}), "/api/items?page="+strconv.Itoa(number)))
		if got.Data == nil || len(got.Data) != 0 || got.Meta.Pages != 0 || !got.Links.Next.IsNull() {
			t.Fatal("empty page fabricated data/total/link")
		}
		if got.Links.Previous.IsNull() != (number == 1) {
			t.Fatal("beyond-last backward navigation lost")
		}
	}
}
func TestPageInputFailuresDoNotCallService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw    string
		status int
		path   string
	}{
		{"page=0", 422, "/query/page"}, {"page=-1", 422, "/query/page"}, {"per_page=0", 422, "/query/per_page"}, {"per_page=101", 422, "/query/per_page"},
		{"page=" + strconv.Itoa(math.MaxInt) + "&per_page=2", 422, "/query"},
		{"page=bad", 400, "/query/page"}, {"page=1&page=2", 400, "/query/page"}, {"per_page=", 400, "/query/per_page"}, {"unknown=secret", 400, "/query"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			response := serve(t, numbered(pagination.DefaultConfig()).Handle(func(context.Context, pageInput) (query.Page[Item], error) {
				t.Error("bad input reached service")
				return query.Page[Item]{}, nil
			}), "/items?"+tc.raw)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.path) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
func TestCustomNamesAndFilterValidationCompose(t *testing.T) {
	t.Parallel()
	config := pagination.DefaultConfig()
	config.NumberParam = "p"
	config.SizeParam = "limit"
	endpoint := numbered(config).WithFiltersValidation(validation.DefineField("q", func(f filters) value.Optional[string] { return f.Term }).Rules(validation.Optional(validation.MinLength[string](3))))
	response := serve(t, endpoint.Handle(func(context.Context, pageInput) (query.Page[Item], error) {
		t.Error("bad filter accepted")
		return query.Page[Item]{}, nil
	}), "/items?q=x&p=1&limit=2")
	if response.Code != 422 || !strings.Contains(response.Body.String(), "/query/q") || strings.Contains(response.Body.String(), "/filters") {
		t.Fatalf("filter path=%s", response.Body)
	}
	response = serve(t, endpoint.Handle(func(context.Context, pageInput) (query.Page[Item], error) {
		t.Error("page bounds replaced by filters")
		return query.Page[Item]{}, nil
	}), "/items?q=okay&p=0")
	if response.Code != 422 || !strings.Contains(response.Body.String(), "/query/p") {
		t.Fatal("custom page rule lost")
	}
	location, err := endpoint.URL(t.Context(), foundryhttp.NoPath{}, filters{}, query.PageRequest{Number: 2, Size: 7})
	if err != nil || location != "/items?limit=7&p=2" {
		t.Fatalf("URL=%q err=%v", location, err)
	}
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{}
	for _, param := range info.Query {
		if text, set := param.DefaultURL.Get(); set {
			defaults[param.Name] = text
		}
	}
	if !reflect.DeepEqual(defaults, map[string]string{"p": "1", "limit": "20"}) {
		t.Fatalf("defaults=%v", defaults)
	}
}
func TestServicePageFailuresDoNotPublishSuccess(t *testing.T) {
	t.Parallel()
	for _, page := range []query.Page[Item]{
		{}, {Number: 2, Size: 20}, {Number: 1, Size: 2}, {Number: 1, Size: 20, Total: 1, Pages: 0}, {Number: 1, Size: 20, Total: -1}, {Number: 1, Size: 20, Items: make([]Item, 21)},
	} {
		response := serve(t, numbered(pagination.DefaultConfig()).Handle(func(context.Context, pageInput) (query.Page[Item], error) { return page, nil }), "/items")
		if response.Code != 500 || strings.Contains(response.Body.String(), `"data"`) {
			t.Fatalf("inconsistent result published: %d %s", response.Code, response.Body)
		}
	}
	private := errors.New("private service error")
	response := serve(t, numbered(pagination.DefaultConfig()).Handle(func(context.Context, pageInput) (query.Page[Item], error) { return query.Page[Item]{}, private }), "/items")
	if response.Code != 500 || strings.Contains(response.Body.String(), private.Error()) {
		t.Fatal("private service failure leaked")
	}
}
func TestSimplePagesDoNotInventTotals(t *testing.T) {
	t.Parallel()
	endpoint := pagination.DefineSimple(route(), filterQuery(), itemJSON(), pagination.DefaultConfig())
	response := serve(t, endpoint.Handle(func(_ context.Context, in pageInput) (query.SimplePage[Item], error) {
		return query.SimplePage[Item]{Items: []Item{{Label: "A"}, {Label: "B"}}, Number: in.Page.Number, Size: in.Page.Size, HasMore: true}, nil
	}), "/items?page=2&per_page=2&q=search")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	var got pagination.SimpleResponse[Item]
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Links.Next.IsNull() || got.Links.Previous.IsNull() || !got.Meta.HasMore {
		t.Fatal("simple navigation lost")
	}
	if strings.Contains(response.Body.String(), "total") || strings.Contains(response.Body.String(), "last_page") {
		t.Fatal("simple page invented totals")
	}
	if _, err := pagination.SimpleJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response); err != nil {
		t.Fatal(err)
	}
	response = serve(t, endpoint.Handle(func(context.Context, pageInput) (query.SimplePage[Item], error) {
		return query.SimplePage[Item]{Number: 1, Size: 20, HasMore: true}, nil
	}), "/items")
	if response.Code != 500 {
		t.Fatal("impossible lookahead metadata accepted")
	}
}
func TestPaginationPublicLinksUseApprovedOrigin(t *testing.T) {
	t.Parallel()
	config := pagination.DefaultConfig()
	config.Links = pagination.PublicLinks
	handler := func(_ context.Context, in pageInput) (query.Page[Item], error) {
		return query.Page[Item]{Number: in.Page.Number, Size: in.Page.Size, Total: 21, Pages: 2}, nil
	}
	// A public policy approves this native HTTP origin, then chooses its configured
	// canonical HTTPS base. Missing policy cannot trust an arbitrary request Host.
	endpoint := numbered(config)
	missing := serve(t, endpoint.Handle(handler), "http://evil.example/items")
	if missing.Code != 500 {
		t.Fatal("unapproved origin used")
	}
	middleware := foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{"http://app.example", "https://app.example"}, Canonical: value.Set(foundryhttp.Origin("https://app.example"))})
	got := decodeNumbered(t, serve(t, endpoint.WithMiddleware(middleware).Handle(handler), "http://app.example/items"))
	next, _ := got.Links.Next.Get()
	if !strings.HasPrefix(next, "https://app.example/items?") {
		t.Fatalf("unapproved link=%s", next)
	}
	router, err := foundryhttp.NewRouter(endpoint.WithMiddleware(middleware).Handle(handler))
	if err != nil {
		t.Fatal(err)
	}
	var _ stdhttp.Handler = router
}
func TestInvalidPaginationDeclarationsFailAssembly(t *testing.T) {
	t.Parallel()
	configurations := []pagination.Config{{}, pagination.DefaultConfig(), pagination.DefaultConfig(), pagination.DefaultConfig()}
	configurations[1].MaximumSize = query.MaxPageSize + 1
	configurations[2].NumberParam = "per_page"
	configurations[3].DefaultSize = 101
	for _, config := range configurations {
		endpoint := numbered(config)
		if endpoint.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
		if _, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, pageInput) (query.Page[Item], error) { return query.Page[Item]{}, nil })); err == nil {
			t.Fatal("invalid config assembled")
		}
	}
	if _, err := foundryhttp.NewRouter(numbered(pagination.DefaultConfig()).Handle(nil)); err == nil {
		t.Fatal("nil handler accepted")
	}
	overlap := foundryhttp.DefineQuery(foundryhttp.QueryParam("page", foundryhttp.StringQuery[string](), func(f *Item) *string { return &f.Label }))
	if pagination.DefineNumbered(route(), overlap, itemJSON(), pagination.DefaultConfig()).Validate() == nil {
		t.Fatal("overlapping filter/pagination name accepted")
	}
	if pagination.DefineNumbered(route(), filterQuery(), contract.JSON[Item]{}, pagination.DefaultConfig()).Validate() == nil {
		t.Fatal("invalid item contract accepted")
	}
}
