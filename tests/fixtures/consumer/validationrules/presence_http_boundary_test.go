package validationrules_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/validationrules"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestHTTPPresenceRejectsInvalidRepresentationBeforeRules(t *testing.T) {
	fields := validationrules.PresenceInputValidationFields()
	server, calls := presenceServer(t, presenceEndpoint().WithBodyValidation(fields.Label.Rules(validation.Prohibited[string]())))
	for _, tc := range []struct{ name, query, body, path string }{
		{"null_text_without_nullable", "", "{\"label\":null}", "/body/label"},
		{"null_integer_without_nullable", "", "{\"count\":null}", "/body/count"},
		{"null_boolean_without_nullable", "", "{\"active\":null}", "/body/active"},
		{"duplicate_json", "", "{\"count\":0,\"count\":1}", ""},
		{"unknown_json", "", "{\"missing\":1}", "/body"},
		{"null_integer_query", "count=null", "{}", "/query/count"},
		{"empty_integer_query", "count=", "{}", "/query/count"},
		{"noncanonical_integer_query", "count=01", "{}", "/query/count"},
		{"noncanonical_boolean_query", "active=0", "{}", "/query/active"},
		{"duplicate_query", "count=0&count=1", "{}", "/query/count"},
		{"unknown_query", "missing=1", "{}", "/query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, failure := sendPresence(t, server, tc.query, tc.body)
			if status != 400 || failure.Status != 400 || failure.Code != foundryhttp.BadRequest {
				t.Fatalf("malformed transport became presence rejection: %d %+v", status, failure)
			}
			if tc.path != "" && (len(failure.Issues) != 1 || failure.Issues[0].Path != tc.path) {
				t.Fatalf("malformed field path: %+v", failure.Issues)
			}
			if count, _ := calls.snapshot(); count != 0 {
				t.Fatal("invalid representation reached domain handler")
			}
		})
	}
}

func TestHTTPPresenceConditionalTriggersUseTheirDeclaredSource(t *testing.T) {
	server, calls := presenceServer(t, presenceEndpoint().WithBodyValidation(validationrules.RequiredWithRules()))
	for _, tc := range []struct {
		name, query, body string
		reject            bool
	}{
		{"omitted", "", "{}", false},
		{"query_zero_is_not_body_presence", "count=0", "{}", false},
		{"query_false_is_not_body_presence", "active=false", "{}", false},
		{"body_zero_requires_label", "", "{\"count\":0}", true},
		{"body_false_requires_label", "", "{\"active\":false}", true},
		{"query_label_cannot_fill_body_label", "label=Query", "{\"count\":0}", true},
		{"supplied_empty_label", "", "{\"active\":false,\"label\":\" \"}", true},
		{"body_label_with_zero", "label=Query", "{\"count\":0,\"label\":\"Body\"}", false},
		{"body_label_with_false", "", "{\"active\":false,\"label\":\"Body\"}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := calls.snapshot()
			status, failure := sendPresence(t, server, tc.query, tc.body)
			after, received := calls.snapshot()
			if tc.reject {
				assertPresenceRejection(t, status, failure, "/body/label", "required")
				if after != before {
					t.Fatal("conditional rejection reached domain handler")
				}
			} else if status != 204 || after != before+1 {
				t.Fatalf("conditional success: status=%d failure=%+v", status, failure)
			} else if tc.name == "body_label_with_zero" &&
				(!reflect.DeepEqual(received.Body.Label, value.Set("Body")) || !reflect.DeepEqual(received.Query.Label, value.Set("Query"))) {
				t.Fatal("same-name query and body values were merged")
			}
		})
	}
}

func TestHTTPPresenceSameNameSourcesKeepIndependentDiagnostics(t *testing.T) {
	fields := validationrules.PresenceInputValidationFields()
	rule := fields.Label.Rules(validation.Required[string]())
	server, calls := presenceServer(t, presenceEndpoint().WithQueryValidation(rule).WithBodyValidation(rule))
	status, failure := sendPresence(t, server, "", "{}")
	if status != 422 || len(failure.Issues) != 2 || failure.Issues[0].Path != "/query/label" || failure.Issues[1].Path != "/body/label" {
		t.Fatalf("source diagnostics: %d %+v", status, failure)
	}
	for _, tc := range []struct{ query, body, path string }{
		{"label=Query", "{}", "/body/label"},
		{"", "{\"label\":\"Body\"}", "/query/label"},
	} {
		status, failure = sendPresence(t, server, tc.query, tc.body)
		assertPresenceRejection(t, status, failure, tc.path, "required")
	}
	if count, _ := calls.snapshot(); count != 0 {
		t.Fatal("incomplete sources reached handler")
	}
	status, failure = sendPresence(t, server, "label=Query", "{\"label\":\"Body\"}")
	count, received := calls.snapshot()
	if status != 204 || count != 1 || !reflect.DeepEqual(received.Query.Label, value.Set("Query")) || !reflect.DeepEqual(received.Body.Label, value.Set("Body")) {
		t.Fatalf("independent source success: %d %+v %+v", status, failure, received)
	}
}

func TestHTTPPresenceCollectionContentRulesKeepItemPaths(t *testing.T) {
	fields := validationrules.PresenceInputValidationFields()
	rule := fields.Tags.Rules(validation.Required(validation.Each[[]string](validation.NonBlank[string]())))
	server, calls := presenceServer(t, presenceEndpoint().WithBodyValidation(rule))
	status, failure := sendPresence(t, server, "", "{\"tags\":[\"one\",\" \"]}")
	if status != 422 || len(failure.Issues) != 1 || failure.Issues[0].Path != "/body/tags/1" {
		t.Fatalf("indexed content error: %d %+v", status, failure)
	}
	status, failure = sendPresence(t, server, "", "{\"tags\":[]}")
	assertPresenceRejection(t, status, failure, "/body/tags", "required")
	if count, _ := calls.snapshot(); count != 0 {
		t.Fatal("invalid collection reached handler")
	}
}
