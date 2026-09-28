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

type publicService struct{ observed chan string }

func (s publicService) Update(ctx context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	relative, err := httpmiddleware.Update.URL(ctx, input.Path, input.Query)
	if err != nil {
		return httpdto.UserResponse{}, err
	}
	absolute, err := foundryhttp.PublicURL(ctx, relative)
	if err != nil {
		return httpdto.UserResponse{}, err
	}
	s.observed <- absolute
	email, _ := input.Body.Email.Get()
	return httpdto.UserResponse{ID: input.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestPublicURLInsideTypedServiceOverRealHTTP(t *testing.T) {
	observed := make(chan string, 2)
	handler, err := httpsecurity.PublicHandler(publicService{observed: observed})
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
	relative, err := httpmiddleware.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ scheme, host, hsts string }{
		{"https", "app.example.test", "max-age=86400"}, {"http", "alias.example.test", ""},
	} {
		r, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+relative, strings.NewReader(`{"email":"member@example.test"}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", test.scheme)
		r.Header.Set("X-Forwarded-Host", test.host)
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		var body httpdto.UserResponse
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || body.ID != id || response.Header.Get("Strict-Transport-Security") != test.hsts {
			t.Fatalf("typed public transport failed: %d %+v", response.StatusCode, body)
		}
		select {
		case absolute := <-observed:
			if absolute != "https://app.example.test"+relative {
				t.Fatalf("wrong service URL %q", absolute)
			}
		default:
			t.Fatal("typed service did not receive public origin")
		}
	}
	r, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "evil.example.test")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("unknown host reached router: %d", response.StatusCode)
	}
}
