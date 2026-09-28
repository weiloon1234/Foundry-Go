package httpkernel

import (
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type routeMember struct{}
type memberPath struct{ Member model.ID[routeMember] }

var memberShow = foundryhttp.DefineRoute(
	foundryhttp.RouteSpec{ID: "members.show", Method: foundryhttp.GET, Access: foundryhttp.Public},
	foundryhttp.DefinePath("/members/{member}",
		foundryhttp.Param("member", foundryhttp.ModelIDPath[routeMember](), func(path *memberPath) *model.ID[routeMember] { return &path.Member }),
	),
).Within(foundryhttp.DefineScope("/api", "api"))

// This fixture explicitly uses the raw payload adapter. Typed request/response
// DTO discovery is the next HTTP slice; path identity is already compiler checked.
func TestTypedNamedRoutesFromIndependentConsumer(t *testing.T) {
	id, err := model.NewID[routeMember]()
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(memberShow.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, path memberPath) {
		info, ok := foundryhttp.MatchedRoute(r.Context())
		if !ok || info.ID != memberShow.ID() || !info.Raw {
			t.Error("route metadata did not match its declaration")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(struct {
			ID model.ID[routeMember] `json:"id"`
		}{ID: path.Member}); err != nil {
			t.Error(err)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	location, err := memberShow.URL(memberPath{Member: id})
	if err != nil || location != "/api/members/"+id.String() {
		t.Fatalf("typed URL: %q %v", location, err)
	}
	client := server.Client()
	client.Timeout = 2 * time.Second
	response, err := client.Get(server.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		ID model.ID[routeMember] `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || body.ID != id {
		t.Fatal("typed path did not reach raw consumer handler")
	}
	request, err := stdhttp.NewRequestWithContext(t.Context(), "DELETE", server.URL+location, nil)
	if err != nil {
		t.Fatal(err)
	}
	denied, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Body.Close()
	var failure foundryhttp.ErrorResponse
	if err := json.NewDecoder(denied.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if denied.StatusCode != 405 || failure.Code != foundryhttp.MethodNotAllowed || denied.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("typed failure: %+v", failure)
	}
	head, err := client.Head(server.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	data, err := io.ReadAll(head.Body)
	if err != nil || len(data) != 0 || head.StatusCode != 200 {
		t.Fatalf("HEAD wire response: status %d bytes %d err %v", head.StatusCode, len(data), err)
	}
}
