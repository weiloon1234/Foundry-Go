package http

import (
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestErrorClassificationNeverPublishesInternalMessages(t *testing.T) {
	private := errors.New("private-credential-marker")
	for _, test := range []struct {
		name   string
		err    error
		code   ErrorCode
		status int
	}{
		{"plain internal cause", private, InternalError, 500},
		{"framework service missing", fault.Wrap(fault.Missing, "private-credential-marker", private), InternalError, 500},
		{"explicit not found", NotFound, NotFound, 404},
		{"wrapped code", fmt.Errorf("private-credential-marker: %w", Forbidden), Forbidden, 403},
		{"outer classification", BadRequest.WithCause(NotFound), BadRequest, 400},
		{"joined classification order", errors.Join(Conflict, Unavailable.WithCause(private)), Conflict, 409},
		{"unknown code", ErrorCode("private-credential-marker"), InternalError, 500},
		{"unknown with cause", ErrorCode("private-credential-marker").WithCause(private), InternalError, 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
			recorder := httptest.NewRecorder()
			if err := WriteError(recorder, request, test.err); err != nil {
				t.Fatal(err)
			}
			var payload ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Code != test.code || payload.Status != test.status || recorder.Code != test.status {
				t.Fatalf("classification: %#v", payload)
			}
			if strings.Contains(recorder.Body.String(), "private-credential-marker") {
				t.Fatal("internal cause reached the public response")
			}
		})
	}
	wrapped := Forbidden.WithCause(private)
	if !errors.Is(wrapped, private) || !errors.Is(wrapped, Forbidden) || strings.Contains(wrapped.Error(), "private-credential-marker") {
		t.Fatal("typed HTTP error did not retain a safely formatted internal cause")
	}
}

func TestRuntimeUsesImmutableErrorCatalog(t *testing.T) {
	definitions := ErrorDefinitions()
	seen := make(map[ErrorCode]bool)
	for _, definition := range definitions {
		if seen[definition.Code] {
			t.Fatalf("duplicate client error contract %s", definition.Code)
		}
		seen[definition.Code] = true
		payload, err := errorResponse(t.Context(), definition.Code)
		if err != nil {
			t.Fatal(err)
		}
		if payload.Status != definition.Status || payload.Message != definition.Message || payload.Code != definition.Code {
			t.Fatal("runtime disagrees with exported error catalog")
		}
	}
	definitions[0].Status = 200
	definitions[0].Message = "mutated"
	if payload, err := errorResponse(t.Context(), BadRequest); err != nil || payload.Status != 400 || payload.Message == "mutated" {
		t.Fatal("consumer changed the runtime error catalog")
	}
}

type sliceFailure []string

func (sliceFailure) Error() string { return "slice failure" }

func TestErrorIdentitySupportsNonComparableCauses(t *testing.T) {
	cause := sliceFailure{"private-credential-marker"}
	wrapped := BadRequest.WithCause(cause)
	if !errors.Is(wrapped, wrapped) || !errors.Is(wrapped, BadRequest) {
		t.Fatal("HTTP error did not preserve its own identity")
	}
	var extracted sliceFailure
	if !errors.As(wrapped, &extracted) || len(extracted) != 1 || extracted[0] != cause[0] {
		t.Fatal("HTTP error did not preserve its non-comparable cause")
	}
}

func TestErrorHeadersPreservePoliciesAndDiscardOldRepresentation(t *testing.T) {
	recorder := httptest.NewRecorder()
	for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Disposition", "Content-Range", "ETag", "Last-Modified", "Trailer", "Trailer:X-Old", "Transfer-Encoding"} {
		recorder.Header().Set(name, "stale")
	}
	for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only", "Access-Control-Allow-Origin", "Set-Cookie", "Allow"} {
		recorder.Header().Set(name, "preserved")
	}
	origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "correlated"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, stdhttp.MethodGet, "/", nil)
	recorder.Header().Set(RequestIDHeader, "spoofed")
	if err := WriteError(recorder, request, BadRequest); err != nil {
		t.Fatal(err)
	}
	for _, values := range recorder.Header() {
		for _, value := range values {
			if value == "stale" {
				t.Fatal("error retained headers for a different representation")
			}
		}
	}
	for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only", "Access-Control-Allow-Origin", "Set-Cookie", "Allow"} {
		if recorder.Header().Get(name) != "preserved" {
			t.Fatalf("error removed policy header %s", name)
		}
	}
	if recorder.Header().Get(RequestIDHeader) != "correlated" {
		t.Fatal("error correlation header did not use the same attribution as JSON")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("error response missing cache/content policy")
	}
}

func TestHeadAndNilErrorResponse(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodHead, "/", nil)
	recorder := httptest.NewRecorder()
	if err := WriteError(recorder, request, NotFound); err != nil || recorder.Code != 404 || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD error emitted a body: %d %s %v", recorder.Code, recorder.Body.String(), err)
	}
	if err := WriteError(recorder, request, nil); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil error accepted: %v", err)
	}
}
