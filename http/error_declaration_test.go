package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func errorEndpoint(id RouteID, path string) Endpoint[NoPath, NoQuery, NoBody, NoContent] {
	return DefineEndpoint(DefineRoute(RouteSpec{ID: id, Method: GET, Access: Public}, StaticPath(path)), EmptyQuery(), EmptyBody(), EmptyResponse(204))
}
func returnHTTPError(err error) Handler[NoPath, NoQuery, NoBody, NoContent] {
	return func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) { return NoContent{}, err }
}

func TestApplicationErrorDeclarationAndCause(t *testing.T) {
	t.Parallel()
	declaration := DefineError("seats.unavailable", 409, "The selected seat is no longer available.")
	info, err := declaration.Description()
	if err != nil {
		t.Fatal(err)
	}
	private := sliceFailure{"private-credential-marker"}
	wrapped := declaration.WithCause(private)
	if !errors.Is(wrapped, declaration) || !errors.Is(wrapped, wrapped) || wrapped.Error() != "seats.unavailable" {
		t.Fatal("lost safe identity")
	}
	var cause sliceFailure
	var selected ErrorDeclaration
	// Ordinary As finds the retained cause; the embedded declaration is an Is
	// target, while returning a declaration directly retains its concrete type.
	if !errors.As(wrapped, &cause) || !errors.As(fmt.Errorf("wrapped: %w", declaration), &selected) || selected != declaration {
		t.Fatal("lost typed error or private cause")
	}
	for _, returned := range []error{declaration, wrapped, fmt.Errorf("private-credential-marker: %w", wrapped), errors.Join(wrapped, Forbidden)} {
		w := httptest.NewRecorder()
		if err := WriteError(w, httptest.NewRequest("GET", "/", nil), returned); err != nil {
			t.Fatal(err)
		}
		payload := decodeFailure(t, w)
		if payload.Code != info.Code || payload.Status != info.Status || payload.Message != info.Message || w.Code != info.Status {
			t.Fatalf("runtime/metadata mismatch: %+v", payload)
		}
		if strings.Contains(w.Body.String(), "private-credential-marker") {
			t.Fatal("cause was exposed")
		}
	}
	info.Message = "changed"
	again, _ := declaration.Description()
	if again.Message == info.Message {
		t.Fatal("description aliases runtime")
	}
	if payload, err := errorResponse(t.Context(), BadRequest.WithCause(wrapped)); err != nil || payload.Code != BadRequest {
		t.Fatal("outer code lost precedence")
	}
	if payload, err := errorResponse(t.Context(), ErrorCode("seats.unavailable")); err != nil || payload.Code != InternalError {
		t.Fatal("a raw custom code bypassed declaration")
	}
}

func TestInvalidApplicationErrorsNeverBecomePublic(t *testing.T) {
	t.Parallel()
	for _, declaration := range []ErrorDeclaration{
		{}, DefineError("", 409, "message"), DefineError("bad code", 409, "message"),
		DefineError("bad", 200, "message"), DefineError("bad", 600, "message"),
		DefineError("bad", 409, "  "), DefineError("bad", 409, "\x00"),
		DefineError("bad", 409, string([]byte{255})), DefineError("bad", 409, strings.Repeat("x", maxErrorMessageBytes+1)),
		DefineError(NotFound, 404, "different"),
	} {
		if declaration.Validate() == nil {
			t.Fatal("invalid declaration accepted")
		}
		if info, err := declaration.Description(); err == nil || info != (ErrorDefinition{}) {
			t.Fatal("invalid metadata escaped")
		}
		if declaration.Error() != string(InternalError) {
			t.Fatal("invalid formatting exposed declaration")
		}
		for _, failure := range []error{declaration, declaration.WithCause(Forbidden)} {
			payload, err := errorResponse(t.Context(), failure)
			if err != nil || payload.Code != InternalError || payload.Message != "Internal server error" {
				t.Fatal("invalid declaration classified as public")
			}
		}
		if router, err := NewRouter(errorEndpoint("bad", "/bad").WithErrors(declaration).Handle(returnHTTPError(nil))); err == nil || router != nil {
			t.Fatal("invalid endpoint partially assembled")
		}
	}
	// A nil error pointer stays inside the existing classification containment.
	var typedNil *declaredResponseError
	if payload, err := errorResponse(t.Context(), typedNil); err == nil || payload.Code != InternalError {
		t.Fatal("nil custom error escaped containment")
	}
}

func TestEndpointErrorsConstrainResponsesAndOwnMetadata(t *testing.T) {
	t.Parallel()
	expected := DefineError("seats.unavailable", 409, "The seat is unavailable.")
	other := DefineError("seats.unavailable", 403, "different meaning")
	input := []ErrorDeclaration{expected}
	base := errorEndpoint("seats.reserve", "/seats")
	declared := base.WithErrors(input...)
	input[0] = other
	plain, _ := base.Description()
	if len(plain.Errors) != 0 {
		t.Fatal("WithErrors mutated base")
	}
	for _, tc := range []struct {
		name     string
		err      error
		code     ErrorCode
		declared bool
	}{
		{"declared", expected, "seats.unavailable", true},
		{"wrapped", expected.WithCause(errors.New("private")), "seats.unavailable", true},
		{"unknown", DefineError("seats.other", 409, "different"), InternalError, true},
		{"changed definition", other, InternalError, true},
		{"no declarations", expected, InternalError, false},
		{"builtin", Forbidden, Forbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := declared
			if !tc.declared {
				endpoint = base
			}
			router, err := NewRouter(endpoint.Handle(returnHTTPError(tc.err)))
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/seats", nil))
			if payload := decodeFailure(t, w); payload.Code != tc.code {
				t.Fatalf("response disagrees with declared contract: %+v", payload)
			}
			if tc.declared {
				snapshot := router.Endpoints()
				snapshot[0].Errors[0].Message = "mutated"
				if router.Endpoints()[0].Errors[0].Message == "mutated" {
					t.Fatal("endpoint metadata aliases declaration")
				}
			}
		})
	}
	// Native GET-to-HEAD behavior retains status and omits the error body.
	router, err := NewRouter(declared.Handle(returnHTTPError(expected)))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("HEAD", "/seats", nil))
	if w.Code != 409 || w.Body.Len() != 0 {
		t.Fatal("HEAD custom failure changed transport behavior")
	}
}

func TestRouterErrorCatalogRejectsConflictsAndAllowsReuse(t *testing.T) {
	t.Parallel()
	unavailable := DefineError("seats.unavailable", 409, "Unavailable")
	gone := DefineError("seats.gone", 410, "Gone")
	register := func(id RouteID, path string, declarations ...ErrorDeclaration) RouteRegistration {
		return errorEndpoint(id, path).WithErrors(declarations...).Handle(returnHTTPError(nil))
	}
	if router, err := NewRouter(register("a", "/a", unavailable, unavailable)); !errors.Is(err, fault.Duplicate) || router != nil {
		t.Fatal("duplicate declaration accepted")
	}
	if router, err := NewRouter(register("a", "/a", unavailable), register("b", "/b", DefineError("seats.unavailable", 410, "Different"))); !errors.Is(err, fault.Conflict) || router != nil {
		t.Fatal("conflicting global error code accepted")
	}
	a, err := NewRouter(register("b", "/b", gone, unavailable), register("a", "/a", unavailable))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRouter(register("a", "/a", unavailable), register("b", "/b", gone, unavailable))
	if err != nil {
		t.Fatal(err)
	}
	catalog := a.ErrorDefinitions()
	if len(catalog) != len(ErrorDefinitions())+2 || !slices.Equal(catalog, b.ErrorDefinitions()) {
		t.Fatal("error catalog is incomplete or nondeterministic")
	}
	catalog[0].Message = "mutated"
	if a.ErrorDefinitions()[0].Message == "mutated" {
		t.Fatal("error catalog is not owned")
	}
	var nilRouter *Router
	if nilRouter.ErrorDefinitions() != nil {
		t.Fatal("nil router advertised a catalog")
	}
}

func TestDeclaredErrorsApplyBeforeRouteMiddleware(t *testing.T) {
	t.Parallel()
	failure := DefineError("seats.closed", 409, "Reservations are closed.")
	middleware := DefineMiddleware("seats.reject", func(stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if err := WriteError(w, r, failure); err != nil {
				t.Error(err)
			}
		}), nil
	})
	for _, declared := range []bool{false, true} {
		endpoint := errorEndpoint("seats.reserve", "/reserve").WithMiddleware(middleware)
		expected := InternalError
		if declared {
			endpoint = endpoint.WithErrors(failure)
			expected = "seats.closed"
		}
		router, err := NewRouter(endpoint.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
			t.Error("rejection middleware called domain handler")
			return NoContent{}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/reserve", nil))
		if payload := decodeFailure(t, w); payload.Code != expected {
			t.Fatalf("route middleware bypassed declared errors: %+v", payload)
		}
	}
}
