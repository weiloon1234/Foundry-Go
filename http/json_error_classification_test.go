package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type jsonTransportFailure struct{}

func (*jsonTransportFailure) Error() string { return "private JSON callback" }
func (*jsonTransportFailure) Unwrap() error { return BadRequest }

type rejectedJSONDecode struct{}

func (*rejectedJSONDecode) UnmarshalJSON([]byte) error { return &jsonTransportFailure{} }

type rejectedJSONSnapshot struct{ cause error }

func (v rejectedJSONSnapshot) MarshalJSON() ([]byte, error) { return nil, v.cause }

type rejectedJSONMapKey struct{}

func (rejectedJSONMapKey) MarshalText() ([]byte, error) { return nil, &jsonTransportFailure{} }
func (*rejectedJSONMapKey) UnmarshalText([]byte) error  { return nil }

type rejectedJSONKeyDecode struct{}

func (rejectedJSONKeyDecode) MarshalText() ([]byte, error) { return []byte("key"), nil }
func (*rejectedJSONKeyDecode) UnmarshalText([]byte) error  { return &jsonTransportFailure{} }

type rejectedJSONKeyCanonical struct{ decoded bool }

func (k rejectedJSONKeyCanonical) MarshalText() ([]byte, error) {
	if k.decoded {
		return nil, &jsonTransportFailure{}
	}
	return []byte("key"), nil
}
func (k *rejectedJSONKeyCanonical) UnmarshalText([]byte) error {
	k.decoded = true
	return nil
}

func TestJSONDiagnosticCausesKeepHTTP500(t *testing.T) {
	_, decoded := value.ParseJSON[rejectedJSONDecode](`{}`)
	_, snapshot := value.NewJSON(rejectedJSONSnapshot{&jsonTransportFailure{}})
	_, keySnapshot := value.NewJSON(map[rejectedJSONMapKey]bool{{}: true})
	_, path := querycore.JSONMapKey(rejectedJSONMapKey{})
	_, keyDecode := querycore.JSONMapKey(rejectedJSONKeyDecode{})
	_, keyCanonical := querycore.JSONMapKey(rejectedJSONKeyCanonical{})
	for name, failure := range map[string]error{
		"decode": decoded, "snapshot": snapshot, "map snapshot": keySnapshot, "map path": path,
		"map path decoding": keyDecode, "map path canonical encoding": keyCanonical,
	} {
		t.Run(name, func(t *testing.T) {
			var cause *jsonTransportFailure
			if !errors.Is(failure, BadRequest) || !errors.Is(failure, fault.Invalid) || !errors.As(failure, &cause) {
				t.Fatal("JSON failure lost its inspectable cause or validation classification")
			}
			for _, test := range []struct {
				name   string
				err    error
				status int
				code   ErrorCode
			}{
				{"direct", failure, 500, InternalError},
				{"wrapped", fmt.Errorf("wrapped: %w", failure), 500, InternalError},
				{"joined sibling", errors.Join(failure, NotFound), 404, NotFound},
				{"explicit outer", BadRequest.WithCause(failure), 400, BadRequest},
			} {
				t.Run(test.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
						if err := WriteError(w, r, test.err); err != nil {
							t.Fatal(err)
						}
					})
					handler.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
					var response ErrorResponse
					if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if recorder.Code != test.status || response.Code != test.code {
						t.Fatalf("HTTP classification: status %d, code %s", recorder.Code, response.Code)
					}
					control := httptest.NewRecorder()
					if err := WriteError(control, httptest.NewRequest(stdhttp.MethodGet, "/", nil), test.code); err != nil {
						t.Fatal(err)
					}
					if recorder.Body.String() != control.Body.String() {
						t.Fatalf("response bytes changed: %q != %q", recorder.Body.String(), control.Body.String())
					}
					if strings.Contains(recorder.Body.String(), "private") {
						t.Fatal("diagnostic cause disclosed")
					}
				})
			}
		})
	}
	// HTTP's authentication fallback must not reinterpret a retained deadline.
	_, timedOut := value.NewJSON(rejectedJSONSnapshot{context.DeadlineExceeded})
	response, err := errorResponse(t.Context(), timedOut)
	if err != nil || response.Status != 500 || !errors.Is(timedOut, context.DeadlineExceeded) {
		t.Fatal("diagnostic deadline changed transport classification")
	}
}

func TestParseJSONMapKeyValidationKeepsHTTPClassification(t *testing.T) {
	_, decoded := value.ParseJSON[map[rejectedJSONKeyDecode]bool](`{"key":true}`)
	_, canonical := value.ParseJSON[map[rejectedJSONKeyCanonical]bool](`{"key":true}`)
	for name, failure := range map[string]error{"decoding": decoded, "canonical encoding": canonical} {
		t.Run(name, func(t *testing.T) {
			var cause *jsonTransportFailure
			if !errors.Is(failure, BadRequest) || !errors.As(failure, &cause) {
				t.Fatal("direct map-key validation lost its cause")
			}
			recorder := httptest.NewRecorder()
			if err := WriteError(recorder, httptest.NewRequest(stdhttp.MethodGet, "/", nil), failure); err != nil {
				t.Fatal(err)
			}
			control := httptest.NewRecorder()
			if err := WriteError(control, httptest.NewRequest(stdhttp.MethodGet, "/", nil), BadRequest); err != nil {
				t.Fatal(err)
			}
			if recorder.Body.String() != control.Body.String() {
				t.Fatal("direct ParseJSON response bytes changed")
			}
			if recorder.Code != stdhttp.StatusBadRequest {
				t.Fatalf("direct ParseJSON classification changed: HTTP %d", recorder.Code)
			}
		})
	}
}
func TestJSONDiagnosticCausesDoNotSupplyRetryHints(t *testing.T) {
	_, failure := value.NewJSON(rejectedJSONSnapshot{fault.Overloaded})
	response := httptest.NewRecorder()
	control := httptest.NewRecorder()
	if err := WriteError(response, httptest.NewRequest("GET", "/", nil), Unavailable.WithCause(failure)); err != nil {
		t.Fatal(err)
	}
	if err := WriteError(control, httptest.NewRequest("GET", "/", nil), Unavailable); err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != control.Body.String() || response.Header().Get("Retry-After") != control.Header().Get("Retry-After") {
		t.Fatal("diagnostic cause changed response bytes or retry hint")
	}
}
