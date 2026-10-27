package contract

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type privateJSONSourceDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateJSONSourceDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private JSON source diagnostic")
}

func (e *privateJSONSourceDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateJSONSourceDiagnostic) Unwrap() error            { return fault.Conflict }

type diagnosticJSONText string

func (text diagnosticJSONText) MarshalJSON() ([]byte, error) {
	return nil, &privateJSONSourceDiagnostic{exit: text == "goexit"}
}

func (*diagnosticJSONText) UnmarshalJSON(data []byte) error {
	return &privateJSONSourceDiagnostic{exit: strings.Contains(string(data), "goexit")}
}

type DiagnosticJSONDTO struct {
	Text diagnosticJSONText `json:"text"`
}

func diagnosticJSONDescriptor() JSON[DiagnosticJSONDTO] {
	root := dtoRoot[DiagnosticJSONDTO]()
	return DefineJSON[DiagnosticJSONDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{{Name: "text", Type: "text", Required: true}}},
		{ID: "text", Kind: StringKind},
	}})
}

func TestTypedJSONDiagnosticsSurviveContainment(t *testing.T) {
	descriptor := diagnosticJSONDescriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal("invalid diagnostic JSON descriptor:", err)
	}
	for _, stage := range []string{"wire", "shape", "decode panic formatter", "decode goexit formatter", "encode panic formatter", "encode goexit formatter"} {
		t.Run(stage, func(t *testing.T) {
			encoding := strings.HasPrefix(stage, "encode")
			var original error
			result := callback.Isolated("transport", func() error {
				if encoding {
					text := diagnosticJSONText("panic")
					if strings.Contains(stage, "goexit") {
						text = "goexit"
					}
					data, err := descriptor.Encode(t.Context(), DiagnosticJSONDTO{Text: text}, dtoLimits())
					if data != nil {
						t.Error("failed encoding exposed response bytes")
					}
					original = err
					return err
				}
				data := []byte(`{"text":"panic"}`)
				switch stage {
				case "wire":
					data = []byte(`{"private input":`)
				case "shape":
					data = []byte(`{}`)
				case "decode goexit formatter":
					data = []byte(`{"text":"goexit"}`)
				}
				value, err := descriptor.Decode(t.Context(), data, dtoLimits())
				if value != (DiagnosticJSONDTO{}) {
					t.Error("failed decoding exposed a partial DTO")
				}
				original = err
				return err
			})
			want, code := "transport: invalid JSON input", fault.Invalid
			var source error
			var issues []Issue
			if encoding {
				want, code = "transport: invalid JSON output", fault.Internal
				var failure *EncodeError
				if !errors.As(result, &failure) {
					t.Fatal("encoding lost its typed error:", result)
				}
				source, issues = failure, failure.Issues()
			} else {
				var failure *DecodeError
				if !errors.As(result, &failure) {
					t.Fatal("decoding lost its typed error:", result)
				}
				source, issues = failure, failure.Issues()
			}
			if original == nil || !errors.Is(result, original) || !errors.Is(result, source) || !errors.Is(result, code) {
				t.Fatal("JSON containment lost classification or source identity")
			}
			if stage == "shape" && (len(issues) != 1 || issues[0].Path != "/text" || issues[0].Code != RequiredIssue) {
				t.Fatal("JSON containment lost field diagnostics")
			}
			var cause *privateJSONSourceDiagnostic
			if strings.Contains(stage, "formatter") && (!errors.As(result, &cause) || !errors.Is(result, cause) || !errors.Is(result, fault.Conflict)) {
				t.Fatal("JSON containment lost its private codec cause")
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != want {
					t.Fatalf("JSON diagnostic = %q, want %q", got, want)
				}
			}
			if cause != nil && cause.calls.Load() != 0 {
				t.Fatal("JSON containment formatted an application cause")
			}
		})
	}
}
