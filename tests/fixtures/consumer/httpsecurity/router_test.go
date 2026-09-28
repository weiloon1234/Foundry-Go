package httpsecurity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmiddleware"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpsecurity"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{}

func (service) Update(_ context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	email, _ := input.Body.Email.Get()
	return httpdto.UserResponse{ID: input.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestTypedSecurityHeadersOverHTTPAndTLS(t *testing.T) {
	t.Parallel()
	handler, err := httpsecurity.Handler(service{})
	if err != nil {
		t.Fatal(err)
	}
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpmiddleware.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range []*httptest.Server{plain, secure} {
		client := server.Client()
		client.Timeout = 3 * time.Second
		request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+location, strings.NewReader(`{"email":"member@example.test"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-Proto", "https")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var body httpdto.UserResponse
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || body.ID != id || body.Email != "member@example.test" {
			t.Fatalf("typed transport changed: status=%d response=%+v", response.StatusCode, body)
		}
		for name, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "SAMEORIGIN", "Referrer-Policy": "strict-origin-when-cross-origin", "X-Build": "fixture"} {
			if response.Header.Get(name) != want {
				t.Errorf("%s: %q", name, response.Header.Get(name))
			}
		}
		hsts := ""
		if server == secure {
			hsts = "max-age=86400"
		}
		if response.Header.Get("Strict-Transport-Security") != hsts {
			t.Fatal("HSTS ignored native transport boundary")
		}
		missing, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/missing", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err = client.Do(missing)
		if err != nil {
			t.Fatal(err)
		}
		var failure foundryhttp.ErrorResponse
		err = json.NewDecoder(response.Body).Decode(&failure)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 404 || failure.Code != foundryhttp.NotFound || response.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
			t.Fatalf("policy missed structured fallback: %+v", failure)
		}
	}
}
