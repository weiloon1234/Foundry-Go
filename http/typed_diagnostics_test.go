package http

import (
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type privateTransportSourceDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateTransportSourceDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private transport source diagnostic")
}

func (e *privateTransportSourceDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateTransportSourceDiagnostic) Unwrap() error            { return fault.Conflict }

type diagnosticTransportCodec struct{ cause error }

func (c diagnosticTransportCodec) Parse(string) (string, error)  { return "", c.cause }
func (c diagnosticTransportCodec) Format(string) (string, error) { return "", c.cause }

type diagnosticTransportInput struct {
	Text    string
	Details EndpointPatch
}

func TestTypedTransportDiagnosticsSurviveContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, stage := range []string{"query wire", "query missing", "query decode", "query encode", "multipart missing", "multipart text", "multipart JSON"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", stage, exit), func(t *testing.T) {
				cause := &privateTransportSourceDiagnostic{exit: exit}
				parameter := QueryParam("text", diagnosticTransportCodec{cause: cause}, func(input *diagnosticTransportInput) *string { return &input.Text })
				query := DefineQuery(parameter)
				limits := DefaultEndpointLimits()
				multipart := strings.HasPrefix(stage, "multipart")
				want := "transport: invalid query parameters"
				issuePath := "/text"
				var original error
				result := callback.Isolated("transport", func() error {
					if multipart {
						want = "transport: invalid multipart form"
						declaration := DefineMultipart(TextPart(parameter))
						input := multipartInput{text: url.Values{"text": {"private value"}}, counts: []int{1}}
						switch stage {
						case "multipart missing":
							input = multipartInput{counts: []int{0}}
						case "multipart JSON":
							declaration = DefineMultipart(JSONPart("details", endpointPatchJSON(), func(input *diagnosticTransportInput) *EndpointPatch { return &input.Details }))
							input = multipartInput{values: map[string][]multipartValue{"details": {{data: []byte(`{}`)}}}, counts: []int{1}}
							issuePath = "/details/name"
						}
						value, err := declaration.bind(t.Context(), input, limits)
						if value.Text != "" || value.Details.Name != "" || value.Details.Note.IsSet() {
							t.Error("failed multipart binding exposed a partial result")
						}
						original = err
						return err
					}
					if stage == "query encode" {
						text, err := query.Encode(t.Context(), diagnosticTransportInput{Text: "private value"}, limits.Query)
						if text != "" {
							t.Error("failed query encoding exposed output")
						}
						original = err
						return err
					}
					raw := "text=private"
					switch stage {
					case "query wire":
						raw, issuePath = "text=%GG", ""
					case "query missing":
						raw = ""
					}
					value, err := query.Decode(t.Context(), raw, limits.Query)
					if value.Text != "" {
						t.Error("failed query binding exposed a partial result")
					}
					original = err
					return err
				})
				var source error
				var issues []contract.Issue
				if multipart {
					var failure *MultipartError
					if !errors.As(result, &failure) {
						t.Fatal("multipart binding lost its typed error")
					}
					source, issues = failure, failure.Issues()
				} else {
					var failure *QueryError
					if !errors.As(result, &failure) {
						t.Fatal("query binding lost its typed error")
					}
					source, issues = failure, failure.Issues()
				}
				if original == nil || !errors.Is(result, original) || !errors.Is(result, source) || !errors.Is(result, fault.Invalid) || len(issues) != 1 || issues[0].Path != issuePath {
					t.Fatal("transport lost classification, source identity or field diagnostics")
				}
				private := stage == "query decode" || stage == "query encode" || stage == "multipart text"
				if private {
					var retained *privateTransportSourceDiagnostic
					if !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause || !errors.Is(result, fault.Conflict) {
						t.Fatal("transport lost the application codec cause")
					}
				}
				if stage != "query encode" {
					inputSource := "query"
					if multipart {
						inputSource = "body"
					}
					mapped := endpointInputError(t.Context(), inputSource, original)
					var response *responseError
					if !errors.Is(mapped, BadRequest) || !errors.Is(mapped, original) || !errors.As(mapped, &response) || len(response.issues) != 1 || response.issues[0].Path != "/"+inputSource+issuePath {
						t.Fatal("source marker changed endpoint status or issue prefixing")
					}
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("transport diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("transport formatted an application cause")
				}
			})
		}
	}
}
