package capture

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// routeActor models the applications' admin types: developer access is a
// type gate, so a wildcard grant must not reach captured payloads.
type routeActor struct {
	ID     int64
	Type   string
	Grants []string
}

func (a routeActor) FoundryIdentity() (model.Identity, error) { return routeReference(a.ID).Identity() }

func routeReference(id int64) model.Reference[routeActor, int64] {
	return model.NewReference[routeActor]("capture_route_actors", id, codec.Signed[int64]())
}

var developerScope = auth.DefineAccessScope[routeActor]("developer.read")
var developerRead = auth.DefinePermission("developer.read", func(_ context.Context, actor routeActor) (bool, error) {
	return actor.Type == "developer", nil
})

var routeActors = map[string]routeActor{"developer": {ID: 1, Type: "developer"}, "ordinary": {ID: 2, Type: "admin"}, "wildcard": {ID: 3, Type: "superadmin", Grants: []string{"*"}}}

func routeGuard(t *testing.T) (http.GuardBinding[routeActor], auth.AccessScopes[routeActor]) {
	t.Helper()
	scopes, err := auth.NewAccessScopes(developerScope)
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("capture.route.actors", routeReference(0), func(_ context.Context, id int64) (value.Optional[routeActor], error) {
		for _, actor := range routeActors {
			if actor.ID == id {
				return value.Set(actor), nil
			}
		}
		return value.Optional[routeActor]{}, nil
	}, func(context.Context, routeActor) (bool, error) { return true, nil })
	verify := func(_ context.Context, token secret.String) (value.Optional[auth.Proof[routeActor, int64]], error) {
		actor, ok := routeActors[token.Reveal()]
		if !ok {
			return value.Optional[auth.Proof[routeActor, int64]]{}, nil
		}
		proof, err := auth.NewScopedProof(routeReference(actor.ID), auth.Authenticated, scopes)
		return value.Set(proof), err
	}
	guard := auth.DefineGuard("capture.route.guard", provider, auth.DefineStrategy("capture.bearer", verify))
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), developerRead.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := http.NewAuthentication(registry, http.BearerCredential("capture.bearer"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := http.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	return binding, scopes
}

func TestPostgresCaptureRoutesAuthorizeDevelopersOnly(t *testing.T) {
	f := newCaptureFixture(t, true)
	f.store.log.Check(t.Context())
	f.complete(t, snapshot("application/json", []byte(`{"amount":1}`)))
	f.store.log.Flush(t.Context())
	binding, scopes := routeGuard(t)
	registrations, err := Routes(http.DefineScope("/developer", "developer"), f.store, binding, http.Scopes(scopes), http.Permissions(developerRead))
	if err != nil {
		t.Fatal(err)
	}
	router, err := http.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, route := range router.Routes() {
		ids = append(ids, string(route.ID))
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"developer.webhook-captures.list", "developer.webhook-captures.show"}) {
		t.Fatal("viewer route IDs changed", ids)
	}
	list := "/developer/webhook-captures?from=2026-11-11T00:00:00Z"
	show := "/developer/webhook-captures/" + string(testRequestID)
	request := func(location, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(stdhttp.MethodGet, location, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("GET %s as %q: status %d, want %d: %s", location, token, w.Code, status, w.Body.String())
		}
		return w
	}
	for _, location := range []string{list, show} {
		request(location, "", stdhttp.StatusUnauthorized)
		request(location, "ordinary", stdhttp.StatusForbidden)
		request(location, "wildcard", stdhttp.StatusForbidden)
	}
	page := request(list, "developer", stdhttp.StatusOK)
	var decoded struct {
		Data []CapturedRequest `json:"data"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &decoded); err != nil || len(decoded.Data) != 1 || decoded.Data[0].RequestID != testRequestID {
		t.Fatal("developer list changed", err, page.Body.String())
	}
	detail := request(show, "developer", stdhttp.StatusOK)
	for _, w := range []*httptest.ResponseRecorder{page, detail} {
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("viewer response is cacheable", w.Header().Get("Cache-Control"))
		}
	}
	request("/developer/webhook-captures", "developer", stdhttp.StatusBadRequest)
	request("/developer/webhook-captures?from=2026-01-01T00:00:00Z", "developer", stdhttp.StatusBadRequest)
	request("/developer/webhook-captures?from=2026-11-11T00:00:00Z&per_page=101", "developer", stdhttp.StatusUnprocessableEntity)
	request("/developer/webhook-captures/"+string(testID(77)), "developer", stdhttp.StatusNotFound)
}
