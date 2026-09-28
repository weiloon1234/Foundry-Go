package pagination_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cursorSource struct{ Stored string }
type cursorInput = pagination.CursorRequest[foundryhttp.NoPath, filters, cursorSource]

func tokenFor[M any](t *testing.T) query.Cursor[M] {
	t.Helper()
	wire := `{"v":1,"scope":"` + strings.Repeat("a", 64) + `","values":[{"k":"int","t":"42"}]}`
	token, err := query.ParseCursor[M](base64.RawURLEncoding.EncodeToString([]byte(wire)))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func cursorEndpoint(config pagination.CursorConfig) pagination.CursorEndpoint[foundryhttp.NoPath, filters, cursorSource, Item] {
	return pagination.DefineCursor[cursorSource](route(), filterQuery(), itemJSON(), config)
}
func cursorDTO(row cursorSource) (Item, error) { return Item{Label: "display:" + row.Stored}, nil }

func TestCursorEndpointOwnsDefaultsDirectionsAndFilterLinks(t *testing.T) {
	t.Parallel()
	token := tokenFor[cursorSource](t)
	config := pagination.DefaultCursorConfig()
	config.AfterParam = "next"
	config.BeforeParam = "previous"
	config.SizeParam = "limit"
	endpoint := cursorEndpoint(config).Within(foundryhttp.DefineScope("/api", "api"))
	for _, direction := range []string{"", "next", "previous"} {
		target := "/api/items?q=a%2Bb+%2F"
		if direction != "" {
			target += "&" + direction + "=" + url.QueryEscape(token.Token())
		}
		response := serve(t, endpoint.Handle(func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
			if in.Page.Size != 20 || in.Page.After.IsSet() != (direction == "next") || in.Page.Before.IsSet() != (direction == "previous") {
				t.Error("typed direction/default lost")
			}
			if term, _ := in.Filters.Term.Get(); term != "a+b /" {
				t.Error("filter escaping lost")
			}
			return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Items: []cursorSource{{Stored: "raw"}}, Size: in.Page.Size, Next: value.Set(token), Previous: value.Set(token)}, cursorDTO)
		}), target)
		if response.Code != 200 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
		got, err := pagination.CursorJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response)
		if err != nil {
			t.Fatal(err)
		}
		if got.Meta.Size != 20 || len(got.Data) != 1 || got.Data[0].Label != "display:raw" {
			t.Fatal("DTO/meta lost")
		}
		next, _ := got.Links.Next.Get()
		previous, _ := got.Links.Previous.Get()
		for key, location := range map[string]string{"next": next, "previous": previous} {
			parsed, err := url.Parse(location)
			if err != nil {
				t.Fatal(err)
			}
			other := "next"
			if key == other {
				other = "previous"
			}
			if parsed.Path != "/api/items" || parsed.Query().Get(key) != token.Token() || parsed.Query().Has(other) || parsed.Query().Get("q") != "a+b /" || parsed.Query().Get("limit") != "20" {
				t.Fatalf("cursor link=%s", location)
			}
		}
		if strings.Contains(response.Body.String(), "total") || strings.Contains(response.Body.String(), "current_page") {
			t.Fatal("cursor invented offset metadata")
		}
	}
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{}
	for _, field := range info.Query {
		if raw, ok := field.DefaultURL.Get(); ok {
			defaults[field.Name] = raw
		}
	}
	if !reflect.DeepEqual(defaults, map[string]string{"limit": "20"}) {
		t.Fatalf("cursor defaults=%v", defaults)
	}
}

func TestCursorInputFailuresStopBeforeService(t *testing.T) {
	t.Parallel()
	token := tokenFor[cursorSource](t).Token()
	for _, tc := range []struct {
		raw    string
		status int
	}{
		{"after=", 400}, {"after=bad", 400}, {"after=" + token + "&after=" + token, 400},
		{"after=" + token + "&before=" + token, 422}, {"per_page=0", 422}, {"per_page=101", 422}, {"page=1", 400},
	} {
		response := serve(t, cursorEndpoint(pagination.DefaultCursorConfig()).Handle(func(context.Context, cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
			t.Error("bad cursor reached service")
			return pagination.CursorResult[cursorSource, Item]{}, nil
		}), "/items?"+tc.raw)
		if response.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.raw, response.Code, response.Body)
		}
	}
}

func TestCursorMappingKeepsSourceOwnershipAndEmptyShape(t *testing.T) {
	t.Parallel()
	token := tokenFor[cursorSource](t)
	source := query.CursorPage[cursorSource]{Items: []cursorSource{{Stored: "secret"}}, Size: 20, Next: value.Set(token)}
	mapped, err := pagination.MapCursorPage(t.Context(), source, cursorDTO)
	if err != nil || mapped.Size() != 20 {
		t.Fatal(err)
	}
	actual, _ := mapped.Next().Get()
	if actual != token {
		t.Fatal("source-owned cursor changed")
	}
	items := mapped.Items()
	items[0].Label = "changed"
	if mapped.Items()[0].Label != "display:secret" || source.Items[0].Stored != "secret" {
		t.Fatal("mapped container aliases source/accessor output")
	}
	response := serve(t, cursorEndpoint(pagination.DefaultCursorConfig()).Handle(func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Size: in.Page.Size}, cursorDTO)
	}), "/items")
	var got pagination.CursorResponse[Item]
	if response.Code != 200 {
		t.Fatalf("empty page: %d %s", response.Code, response.Body)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data == nil || !got.Links.Next.IsNull() || !got.Links.Previous.IsNull() {
		t.Fatal("empty cursor shape lost")
	}
	cause := errors.New("private mapping failure")
	failed, err := pagination.MapCursorPage(t.Context(), source, func(cursorSource) (Item, error) { return Item{}, cause })
	if !errors.Is(err, cause) || !reflect.DeepEqual(failed, pagination.CursorResult[cursorSource, Item]{}) {
		t.Fatal("mapping failure published partial metadata")
	}
}

func TestCursorEndpointSeparatesInvalidInputAndServerResults(t *testing.T) {
	t.Parallel()
	badInput := (query.CursorRequest[cursorSource]{}).Validate()
	for _, tc := range []struct {
		failure error
		status  int
	}{
		{badInput, 400}, {fault.New(fault.Invalid, "invalid query declaration"), 500}, {errors.New("private infrastructure error"), 500},
	} {
		response := serve(t, cursorEndpoint(pagination.DefaultCursorConfig()).Handle(func(context.Context, cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
			return pagination.CursorResult[cursorSource, Item]{}, tc.failure
		}), "/items")
		if response.Code != tc.status || strings.Contains(response.Body.String(), "private") {
			t.Fatalf("classification=%d %s", response.Code, response.Body)
		}
	}
	for _, zero := range []bool{true, false} {
		response := serve(t, cursorEndpoint(pagination.DefaultCursorConfig()).Handle(func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
			if zero {
				return pagination.CursorResult[cursorSource, Item]{}, nil
			}
			return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Size: in.Page.Size + 1}, cursorDTO)
		}), "/items")
		if response.Code != 500 || strings.Contains(response.Body.String(), `"data"`) {
			t.Fatal("invalid cursor service result published")
		}
	}
}

func TestCursorLinksRequireApprovedPublicOrigins(t *testing.T) {
	t.Parallel()
	token := tokenFor[cursorSource](t)
	config := pagination.DefaultCursorConfig()
	config.Links = pagination.PublicLinks
	endpoint := cursorEndpoint(config)
	handler := func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Items: []cursorSource{{Stored: "raw"}}, Size: in.Page.Size, Next: value.Set(token)}, cursorDTO)
	}
	response := serve(t, endpoint.Handle(handler), "http://evil.example/items")
	if response.Code != 500 {
		t.Fatal("unapproved origin trusted")
	}
	middleware := foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{"https://app.example"}})
	response = serve(t, endpoint.WithMiddleware(middleware).Handle(handler), "https://app.example/items")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "https://app.example/items?") {
		t.Fatalf("approved link: %d %s", response.Code, response.Body)
	}
}

func TestCursorQueryCodecPreservesTypedPosition(t *testing.T) {
	codec := pagination.CursorQuery[cursorSource]()
	token := tokenFor[cursorSource](t)
	raw, err := codec.Format(token)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Parse(raw)
	if err != nil || decoded != token {
		t.Fatal("cursor codec changed its typed position")
	}
	if _, err := codec.Format(query.Cursor[cursorSource]{}); err == nil {
		t.Fatal("zero cursor encoded")
	}
	if _, err := codec.Parse("bad"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestCursorSizeChangesAndInvalidDeclarations(t *testing.T) {
	token := tokenFor[cursorSource](t)
	endpoint := cursorEndpoint(pagination.DefaultCursorConfig())
	response := serve(t, endpoint.Handle(func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		if in.Page.Size != 2 {
			t.Error("explicit size was replaced by default")
		}
		return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Items: []cursorSource{{Stored: "row"}}, Size: in.Page.Size, Next: value.Set(token)}, cursorDTO)
	}), "/items?per_page=2&after="+token.Token())
	if response.Code != 200 {
		t.Fatalf("changed size: %d %s", response.Code, response.Body)
	}
	got, err := pagination.CursorJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := got.Links.Next.Get()
	link, err := url.Parse(next)
	if err != nil || link.Query().Get("per_page") != "2" {
		t.Fatal("navigation lost changed size")
	}
	configs := []pagination.CursorConfig{{}, pagination.DefaultCursorConfig(), pagination.DefaultCursorConfig(), pagination.DefaultCursorConfig()}
	configs[1].AfterParam = configs[1].BeforeParam
	configs[2].MaximumSize = query.MaxPageSize + 1
	configs[3].BeforeParam = ""
	for _, config := range configs {
		if cursorEndpoint(config).Validate() == nil {
			t.Fatal("invalid cursor configuration accepted")
		}
	}
	if _, err := foundryhttp.NewRouter(endpoint.Handle(nil)); err == nil {
		t.Fatal("nil cursor handler accepted")
	}
	if _, err := endpoint.URL(t.Context(), foundryhttp.NoPath{}, filters{}, query.CursorRequest[cursorSource]{Size: 20, After: value.Set(query.Cursor[cursorSource]{})}); err == nil {
		t.Fatal("zero cursor became URL")
	}
}

type cyclicCursorServiceError struct{ visits atomic.Int32 }

func (*cyclicCursorServiceError) Error() string {
	panic("private cursor service error must not be formatted")
}
func (e *cyclicCursorServiceError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}
func TestCursorHandlerErrorInspectionIsBoundedAndNextRequestSucceeds(t *testing.T) {
	cycle := new(cyclicCursorServiceError)
	fail := true
	endpoint := cursorEndpoint(pagination.DefaultCursorConfig())
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		if fail {
			return pagination.CursorResult[cursorSource, Item]{}, cycle
		}
		return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Size: in.Page.Size}, cursorDTO)
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/items", nil))
	if response.Code != 500 || cycle.visits.Load() == 0 || cycle.visits.Load() > 2*256 {
		t.Fatal("cursor error classification did not finish safely")
	}
	fail = false
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/items", nil))
	if response.Code != 200 {
		t.Fatal("cursor error prevented the next request")
	}
}
