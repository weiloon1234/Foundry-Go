package httpvalidation_test

import (
	"context"
	"encoding/json"
	"errors"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpvalidation"
	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type remoteAddresses struct{ calls atomic.Int32 }

func (*remoteAddresses) Validate() error { return nil }
func (r *remoteAddresses) Exists(ctx context.Context, email string) (bool, error) {
	r.calls.Add(1)
	if ctx == nil {
		return false, errors.New("context missing")
	}
	if email == "fail@example.test" {
		return false, errors.New("private upstream failure")
	}
	return email == "taken@example.test", nil
}
func TestAsyncRulesUseRequestLifecycle(t *testing.T) {
	lookup := &remoteAddresses{}
	domain := &service{}
	router, err := httpvalidation.AsyncRouter(domain, lookup)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := localization.ValidationCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
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
		codes  []string
	}{
		{`{"email":"bad","nickname":"CAPS"}`, 422, []string{"foundry.email", "foundry.lowercase"}},
		{`{"email":"taken@example.test"}`, 422, []string{"foundry.unique"}},
		{`{"email":"fail@example.test"}`, 500, nil},
		{`{"email":"free@example.test","nickname":"valid"}`, 200, nil},
		{`{}`, 200, nil},
	} {
		request := httptest.NewRequest("PATCH", location, strings.NewReader(tc.body)).WithContext(t.Context())
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		request.Header.Set("Accept-Language", "ms")
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("status %d want %d: %s", response.Code, tc.status, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "private upstream") {
			t.Fatal("private execution error leaked")
		}
		if tc.status == 422 {
			var failure foundryhttp.ErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if len(failure.Issues) != len(tc.codes) {
				t.Fatal(failure.Issues)
			}
			for i, code := range tc.codes {
				if string(failure.Issues[i].Code) != code {
					t.Fatal(failure.Issues)
				}
			}
			if tc.body == `{"email":"taken@example.test"}` && failure.Issues[0].Message != "Email address sudah digunakan." {
				t.Fatal("async message not localized", failure.Issues)
			}
			if failure.Issues[0].Path != "/body/email" {
				t.Fatal("request field path lost", failure.Issues)
			}
		}
	}
	if lookup.calls.Load() != 3 || domain.calls.Load() != 2 {
		t.Fatal("bail/handler admission changed", lookup.calls.Load(), domain.calls.Load())
	}
	if router.Endpoints()[0].Validation == nil {
		t.Fatal("async rules lost metadata")
	}
}
