package httpmiddleware_test

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
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{}

func (service) Update(_ context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	email, _ := input.Body.Email.Get()
	return httpdto.UserResponse{ID: input.Path.User, Email: email, State: models.StatusActive}, nil
}

func TestScopedMiddlewareWithTypedEndpointOverHTTP(t *testing.T) {
	t.Parallel()
	router, err := httpmiddleware.Router(service{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpmiddleware.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{Latitude: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+location, strings.NewReader(`{"email":"member@example.test"}`))
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
	var output httpdto.UserResponse
	if err := json.NewDecoder(response.Body).Decode(&output); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("X-Matched-Route") != "api.users.update" || output.ID != id || output.Email != "member@example.test" {
		t.Fatalf("middleware changed typed transport: status=%d, response=%+v", response.StatusCode, output)
	}
	info := router.Endpoints()[0]
	if len(info.Route.Middlewares) != 1 || info.Route.Middlewares[0] != httpmiddleware.RouteTrace {
		t.Fatalf("middleware missing from endpoint inspection: %+v", info.Route)
	}
	info.Route.Middlewares[0] = "changed"
	if router.Endpoints()[0].Route.Middlewares[0] != httpmiddleware.RouteTrace {
		t.Fatal("endpoint metadata shares mutable middleware IDs")
	}
}
