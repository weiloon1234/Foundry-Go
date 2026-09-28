package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompletionTextPreservesDocumentation(t *testing.T) {
	for _, documentation := range []any{"Custom getter: Member.AccessEmail", map[string]string{"kind": "markdown", "value": "Custom getter: Member.AccessEmail\nCustom setter: Member.MutateEmail"}} {
		for _, array := range []bool{false, true} {
			items := []any{map[string]any{"label": "Email", "detail": "string", "documentation": documentation}}
			var payload any = map[string]any{"items": items}
			if array {
				payload = items
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(data)
			var output bytes.Buffer
			if err := WriteText(&output, Result{Operation: "complete", Payload: data}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "Email\tstring") || !strings.Contains(output.String(), "Custom getter: Member.AccessEmail") {
				t.Fatalf("completion lost field documentation: %s", output.String())
			}
			if !bytes.Equal(data, before) {
				t.Fatal("rendering changed the JSON response")
			}
		}
	}
}
