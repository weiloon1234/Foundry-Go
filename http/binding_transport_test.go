package http_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type customHandlerTransport struct {
	foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent]
}

func (c customHandlerTransport) Handle(handler foundryhttp.Handler[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent]) foundryhttp.RouteRegistration {
	return c.Endpoint.Handle(handler)
}
func (c customHandlerTransport) Validate() error {
	return foundryhttp.ValidateEndpoint(c.Endpoint)
}
func (c customHandlerTransport) Description() (foundryhttp.EndpointInfo, error) {
	return foundryhttp.InspectEndpoint(c.Endpoint)
}

type customAuthenticatedTransport struct {
	foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, authAccount, foundryhttp.NoContent]
}

func (c customAuthenticatedTransport) Handle(handler func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error)) foundryhttp.RouteRegistration {
	return c.AuthenticatedEndpoint.Handle(handler)
}
func (c customAuthenticatedTransport) Validate() error {
	return foundryhttp.ValidateAuthenticated(c.AuthenticatedEndpoint)
}
func (c customAuthenticatedTransport) Description() (foundryhttp.EndpointInfo, error) {
	return foundryhttp.InspectAuthenticated(c.AuthenticatedEndpoint)
}

func TestAfterValidationNilHandlerReturnsInternalError(t *testing.T) {
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "test.late", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/test-late")),
		foundryhttp.EmptyQuery(),
		foundryhttp.EmptyBody(),
		foundryhttp.EmptyResponse(204),
	)
	transport := foundryhttp.AfterValidation(customHandlerTransport{endpoint})
	registration := transport.HandleBound(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Handler[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], error) {
		return nil, nil
	})
	router, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/test-late", nil))
	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	var failure foundryhttp.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != foundryhttp.InternalError {
		t.Fatalf("code = %s, want %s", failure.Code, foundryhttp.InternalError)
	}
}

func TestAfterAuthenticatedValidationNilHandlerReturnsInternalError(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	authEndpoint := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard)
	lateTransport := foundryhttp.AfterAuthenticatedValidation(customAuthenticatedTransport{authEndpoint})
	registration := lateTransport.HandleBound(func(ctx context.Context, actor authAccount, input authInput) (func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error), error) {
		return nil, nil
	})
	router := newAuthRouter(t, registration)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/profile", nil)
	request.Header.Set("Authorization", "Bearer valid")
	router.ServeHTTP(recorder, request)
	if recorder.Code != 500 {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	var failure foundryhttp.ErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != foundryhttp.InternalError {
		t.Fatalf("code = %s, want %s", failure.Code, foundryhttp.InternalError)
	}
}
