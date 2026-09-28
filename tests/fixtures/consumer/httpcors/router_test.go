package httpcors_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpcors"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmiddleware"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{ calls atomic.Int32 }

func (s *service) Update(_ context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	s.calls.Add(1)
	email, _ := input.Body.Email.Get()
	return httpdto.UserResponse{ID: input.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestGlobalCORSWithGeneratedEndpointOverHTTP(t *testing.T) {
	t.Parallel()
	svc := &service{}
	handler, err := httpcors.Handler(svc)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpmiddleware.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{})
	if err != nil {
		t.Fatal(err)
	}
	// This path has only a PATCH endpoint. The global policy owns its preflight.
	preflight, err := http.NewRequestWithContext(t.Context(), "OPTIONS", server.URL+location, nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", string(httpcors.BrowserOrigin))
	preflight.Header.Set("Access-Control-Request-Method", "PATCH")
	preflight.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	response, err := client.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 || svc.calls.Load() != 0 || response.Header.Get("X-Matched-Route") != "" || response.Header.Get("Access-Control-Allow-Origin") != string(httpcors.BrowserOrigin) || response.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("preflight: status=%d headers=%v", response.StatusCode, response.Header)
	}
	request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+location, strings.NewReader(`{"email":"member@example.test"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", string(httpcors.BrowserOrigin))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result httpdto.UserResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || svc.calls.Load() != 1 || result.ID != id || result.Email != "member@example.test" || response.Header.Get("Access-Control-Expose-Headers") != "X-Matched-Route" || response.Header.Get("X-Matched-Route") != "api.users.update" {
		t.Fatalf("typed response: status=%d value=%+v headers=%v", response.StatusCode, result, response.Header)
	}
	missing, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing.Header.Set("Origin", string(httpcors.BrowserOrigin))
	response, err = client.Do(missing)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var failure foundryhttp.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 404 || failure.Code != foundryhttp.NotFound || response.Header.Get("Access-Control-Allow-Origin") != string(httpcors.BrowserOrigin) {
		t.Fatalf("global sharing missed structured fallback: %+v", failure)
	}
}
