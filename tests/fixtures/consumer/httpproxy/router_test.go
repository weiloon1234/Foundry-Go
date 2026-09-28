package httpproxy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmiddleware"
	"foundry.test/consumer/httpproxy"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{ clients chan netip.Addr }

func (s service) Update(ctx context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	ip := foundryhttp.ClientIP(ctx)
	if attribution.FromContext(ctx).Request().IP != ip {
		return httpdto.UserResponse{}, foundryhttp.InternalError
	}
	s.clients <- ip
	email, _ := input.Body.Email.Get()
	return httpdto.UserResponse{ID: input.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestTypedServiceReceivesResolvedClientAttributionOverHTTP(t *testing.T) {
	t.Parallel()
	svc := service{clients: make(chan netip.Addr, 2)}
	handler, err := httpproxy.Handler(svc)
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
	for _, headers := range []http.Header{
		{"X-Forwarded-For": {"1.1.1.1,198.51.100.8,127.0.0.2"}},
		{"Forwarded": {"for=1.1.1.1,for=198.51.100.8;proto=https,for=127.0.0.2"}, "X-Forwarded-For": {"203.0.113.1"}},
	} {
		request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+location, strings.NewReader(`{"email":"member@example.test"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header = headers
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var output httpdto.UserResponse
		err = json.NewDecoder(response.Body).Decode(&output)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || output.ID != id || output.Email != "member@example.test" {
			t.Fatalf("typed transport changed: status=%d response=%+v", response.StatusCode, output)
		}
		select {
		case ip := <-svc.clients:
			if ip != netip.MustParseAddr("198.51.100.8") {
				t.Fatalf("service trusted a spoofed leftmost IP: %v", ip)
			}
		default:
			t.Fatal("typed service did not execute")
		}
	}
}
