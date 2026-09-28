package httpendpoints_test

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type service struct {
	received chan httpendpoints.UpdateRequest
}

func (s service) Update(_ context.Context, in httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	s.received <- in
	email, _ := in.Body.Email.Get()
	return httpdto.UserResponse{ID: in.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestGeneratedEndpointOverHTTP(t *testing.T) {
	t.Parallel()
	domain := service{received: make(chan httpendpoints.UpdateRequest, 1)}
	router, err := httpendpoints.Router(domain)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	path, err := httpendpoints.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{Latitude: 1.5, Maximum: value.Set(httpquery.Distance(12.5)), Distances: httpquery.Distances{2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := stdhttp.NewRequestWithContext(t.Context(), "PATCH", server.URL+path, strings.NewReader(`{"email":"member@example.test","nickname":null}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var got httpdto.UserResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || got.ID != id || got.Email != "member@example.test" || got.State != models.StatusActive {
		t.Fatalf("response: status=%d %+v", response.StatusCode, got)
	}
	select {
	case in := <-domain.received:
		nickname, set := in.Body.Nickname.Get()
		maximum, _ := in.Query.Maximum.Get()
		if in.Path.User != id || in.Query.Latitude != 1.5 || maximum != 12.5 || len(in.Query.Distances) != 2 || !set || !nickname.IsNull() || in.Body.State.IsSet() {
			t.Fatal("typed source or omission/null semantics changed")
		}
	default:
		t.Fatal("domain service was not invoked")
	}
	info := router.Endpoints()
	if len(info) != 1 || info[0].Route.Raw || info[0].Body.Schema.Root != "foundry.test/consumer/httpdto.UpdateUser" || info[0].Response.Schema.Root != "foundry.test/consumer/httpdto.UserResponse" {
		t.Fatal("inspection differs from runtime contracts")
	}
	// A wire error uses the framework envelope and never reaches the service.
	bad := httptest.NewRequest("PATCH", path, strings.NewReader(`{"email":null}`))
	bad.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, bad)
	var failure foundryhttp.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || len(failure.Issues) != 1 || failure.Issues[0].Path != "/body/email" {
		t.Fatalf("field failure: %+v", failure)
	}
	select {
	case <-domain.received:
		t.Fatal("invalid input reached domain service")
	default:
	}
}
