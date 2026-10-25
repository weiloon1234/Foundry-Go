package http

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rejectedEnumRoundTrip int

type enumRoundTripFailure struct{}

func (*enumRoundTripFailure) Error() string { return "private enum round-trip" }
func (*enumRoundTripFailure) Unwrap() error { return BadRequest }

var enumRoundTripCause = &enumRoundTripFailure{}

func (*rejectedEnumRoundTrip) UnmarshalJSON([]byte) error { return enumRoundTripCause }

type enumDiagnosticEncode struct{ cause error }

func (v enumDiagnosticEncode) MarshalJSON() ([]byte, error) { return nil, v.cause }

func TestEnumRoundTripDiagnosticKeepsHTTPResponseBytes(t *testing.T) {
	descriptor := enum.Describe("http.test", "RejectedEnumRoundTrip", enum.Case[rejectedEnumRoundTrip]{Name: "One", Value: 1})
	failure := descriptor.Validate()
	var cause *enumRoundTripFailure
	if !errors.Is(failure, enumRoundTripCause) || !errors.Is(failure, BadRequest) || !errors.As(failure, &cause) || cause != enumRoundTripCause {
		t.Fatal("enum round-trip cause identity lost")
	}
	_, streamFailure := jsonv2.Marshal(value.List[enumDiagnosticEncode]{{failure}})
	if !errors.Is(streamFailure, BadRequest) {
		t.Fatal("native List lost diagnostic cause identity")
	}
	for _, test := range []struct {
		name    string
		failure error
		control error
	}{
		{"direct", failure, errors.New("enum serialization does not preserve its value")},
		{"wrapped", fmt.Errorf("operation: %w", failure), InternalError},
		{"native List diagnostic", streamFailure, InternalError},
		{"joined", errors.Join(failure, NotFound), NotFound},
		{"explicit", BadRequest.WithCause(failure), BadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
			if err := WriteError(actual, httptest.NewRequest("GET", "/", nil), test.failure); err != nil {
				t.Fatal(err)
			}
			if err := WriteError(expected, httptest.NewRequest("GET", "/", nil), test.control); err != nil {
				t.Fatal(err)
			}
			if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() {
				t.Fatalf("enum error changed response: status %d vs %d, body %q vs %q", actual.Code, expected.Code, actual.Body.String(), expected.Body.String())
			}
		})
	}
}
