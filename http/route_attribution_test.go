package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
)

func TestMatchedRouteIsAttributedWithoutTheRawURL(t *testing.T) {
	var seen attribution.Route
	var present bool
	router, err := NewRouter(staticRoute("accounts.update", PATCH, "/accounts/current").HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		seen, present = attribution.RouteFromContext(r.Context())
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("PATCH", "/accounts/current?token=private", nil))
	if recorder.Code != 204 || !present || seen.Method != "PATCH" || seen.Name != "accounts.update" {
		t.Fatalf("matched route was not attributed: %d %v", recorder.Code, present)
	}
}
