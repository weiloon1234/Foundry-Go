package httpvalidation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpvalidation"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type service struct{ calls atomic.Int32 }

func (s *service) Update(_ context.Context, input httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	s.calls.Add(1)
	return httpdto.UserResponse{ID: input.Path.User, Email: "stored@example.test", State: models.StatusActive}, nil
}

func TestValidationThroughRealHTTP(t *testing.T) {
	t.Parallel()
	domain := &service{}
	router, err := httpvalidation.Router(domain)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpvalidation.Update.URL(t.Context(), httpkernel.UserPath{User: id}, httpquery.NearbyInput{Latitude: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"email":" ","nickname":"x"}`, 422},
		{`{"nickname":null}`, 200},
		{`{}`, 200},
	} {
		request, err := http.NewRequestWithContext(t.Context(), "PATCH", server.URL+location, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != tc.status {
			response.Body.Close()
			t.Fatalf("status %d want %d", response.StatusCode, tc.status)
		}
		if tc.status == 422 {
			var failure foundryhttp.ErrorResponse
			err = json.NewDecoder(response.Body).Decode(&failure)
			if err != nil {
				response.Body.Close()
				t.Fatal(err)
			}
			if len(failure.Issues) != 2 || failure.Issues[0].Path != "/body/email" || failure.Issues[1].Path != "/body/nickname" || failure.Issues[0].Message == "" || failure.Issues[0].Label != "Email address" || failure.Issues[1].Label != "Display name" {
				response.Body.Close()
				t.Fatalf("validation details: %+v", failure)
			}
		}
		response.Body.Close()
	}
	if domain.calls.Load() != 2 {
		t.Fatal("validation changed handler admission")
	}
	if router.Endpoints()[0].Validation == nil {
		t.Fatal("consumer validation metadata missing")
	}
}
