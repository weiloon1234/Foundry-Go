package integrations_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestORMPaginationKeepsTransportIdentity(t *testing.T) {
	// These frozen type names came from the legacy aliases. Transport assignment
	// compatibility is also exercised below through the public HTTP adapters.
	for _, typ := range []reflect.Type{
		reflect.TypeFor[orm.PageResult[models.User]](),
		reflect.TypeFor[orm.SimplePage[models.User]](),
		reflect.TypeFor[orm.Cursor[models.User]](),
		reflect.TypeFor[orm.CursorRequest[models.User]](),
		reflect.TypeFor[orm.CursorPage[models.User]](),
		reflect.TypeFor[orm.CursorInputError](),
	} {
		if !oracleMatch(t, "goway/integrations/pagination/types", oracle.Snapshot(typ.String())) {
			t.Fatalf("transport identity changed: %v", typ)
		}
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"scope":"` + strings.Repeat("a", 64) + `","values":[{"k":"int","t":"42"}]}`))
	if _, err := orm.ParseCursor[models.User](token); err != nil {
		t.Fatal(err)
	}
	// Valid tokens and invalid structures retain the legacy parse result.
	for _, input := range []string{token, "", "bad!"} {
		current, err := orm.ParseCursor[models.User](input)
		if !oracleMatch(t, "goway/integrations/pagination/parse", oracle.Snapshot([]any{err == nil, current.Token()})) {
			t.Fatal("cursor transport changed")
		}
	}
}

func TestORMPagesReachHTTPWithoutExposingModels(t *testing.T) {
	registration := pagination.Handle(integrations.List, func(ctx context.Context, request integrations.Request) (orm.PageResult[integrations.Response], error) {
		return pagination.MapPage(ctx, orm.PageResult[models.User]{Items: []models.User{{Email: "visible@example.test"}}, Number: request.Page.Number, Size: request.Page.Size, Total: 1, Pages: 1}, integrations.Present)
	})
	router, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/orm-users", nil))
	var body pagination.NumberedResponse[integrations.Response]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || !reflect.DeepEqual(body.Data, []integrations.Response{{Email: "visible@example.test"}}) {
		t.Fatalf("ORM HTTP page: %d %s", response.Code, response.Body.String())
	}
	if _, err := pagination.MapCursorPage(t.Context(), orm.CursorPage[models.User]{Items: []models.User{{Email: "cursor@example.test"}}, Size: 20}, integrations.Present); err != nil {
		t.Fatal(err)
	}
	var cursor orm.Cursor[models.User]
	if _, err := pagination.URL(integrations.Cursor, t.Context(), foundryhttp.NoPath{}, integrations.Filters{}, orm.CursorRequest[models.User]{Size: 20, After: value.Set(cursor)}); err == nil {
		t.Fatal("zero cursor accepted")
	}
}
