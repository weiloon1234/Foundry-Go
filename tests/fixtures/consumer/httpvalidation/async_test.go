package httpvalidation_test

import (
	"context"
	"encoding/json"
	"errors"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpvalidation"
	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
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

// ownedAddresses is an application lookup scoped at check time by the route key.
type ownedAddresses struct {
	current validation.Slot[model.ID[models.User]]
	owners  map[string]model.ID[models.User]
}

func (*ownedAddresses) Validate() error { return nil }
func (a *ownedAddresses) Exists(ctx context.Context, email string) (bool, error) {
	ignored, err := a.current.Value(ctx)
	if err != nil {
		return false, err
	}
	owner, found := a.owners[email]
	return found && owner != ignored, nil
}

func TestSlotProvidesRouteKeyToOneDeclaredRule(t *testing.T) {
	first, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	current := validation.NewSlot[model.ID[models.User]]()
	domain := &service{}
	router, err := httpvalidation.OwnEmailRouter(domain, current, &ownedAddresses{current: current, owners: map[string]model.ID[models.User]{"first@example.test": first}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user   model.ID[models.User]
		status int
	}{{first, 200}, {second, 422}} {
		location, err := httpvalidation.Update.URL(t.Context(), httpkernel.UserPath{User: tc.user}, httpquery.NearbyInput{Latitude: 1.5})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("PATCH", location, strings.NewReader(`{"email":"first@example.test"}`)).WithContext(t.Context())
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("status %d want %d: %s", response.Code, tc.status, response.Body.String())
		}
		if tc.status == 422 {
			var failure foundryhttp.ErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if len(failure.Issues) != 1 || failure.Issues[0].Path != "/body/email" || failure.Issues[0].Code != "foundry.unique" {
				t.Fatal(failure.Issues)
			}
		}
	}
	// A slot reader without its provider fails registration, not each request.
	fields := httpdto.UpdateUserValidationFields()
	missing := httpendpoints.Update.WithBodyValidation(fields.Email.Rules(validation.Optional(validation.Requires(current, validation.Unique[string](&ownedAddresses{current: current})))))
	if _, err := foundryhttp.NewRouter(missing.Handle(domain.Update)); err == nil {
		t.Fatal("slot reader registered without its provider")
	}
}
