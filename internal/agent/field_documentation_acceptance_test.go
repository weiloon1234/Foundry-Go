package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealGoplsFieldBehaviorDocumentation(t *testing.T) {
	testkit.TrackExternalInputs(t)
	executable := os.Getenv("FOUNDRY_TEST_GOPLS")
	if executable == "" {
		t.Skip("requires the existing approved gopls executable")
	}
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		var err error
		executable, err = filepath.Abs(executable)
		if err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := filepath.Abs("../../tests/fixtures/consumer")
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		name, file, prefix, symbol, definition string
		documentation                          []string
	}{
		{"sensitive-password-field", "passwords/account_postgres_test.go", "return account.", "Digest", "/passwords/account.go", []string{"Sensitive stored password hash", "password.Codec", "redacted", "Compare-and-swap"}},
		{"raw-field-both", "mutatorqueries/accessors_test.go", "storedEmail := member.", "Email", "/mutatorqueries/member.go", []string{"Custom getter", "AccessEmail", "Custom setter", "MutateEmail", "email_address", "Delivery address."}},
		{"raw-field-getter", "mutatorqueries/accessors_test.go", "if member.", "ID", "/mutatorqueries/member.go", []string{"Custom getter", "AccessID", "ID is the database identity."}},
		{"raw-field-setter", "mutatorqueries/accessors_test.go", "|| member.", "Attempts", "/mutatorqueries/member.go", []string{"Custom setter", "MutateAttempts", "persistence", "Direct field assignment"}},
		{"query-field", "mutatorqueries/mutators_postgres_test.go", "q.Query().Where(f.", "Email", "/mutatorqueries/orm.go", []string{"Custom getter", "AccessEmail", "Custom setter", "MutateEmail", "delivery address."}},
		{"extension-slot-field", "articles/cleanup_postgres_test.go", "if article.", "Title", "/articles/article.go", []string{"Extension slot, not a column", "foundry_model_translations", "ArticleSlots.Title", "From(runtime)", "localized article heading shown to readers"}},
		{"draft-setter", "mutatorqueries/mutators_postgres_test.go", "mutatorqueries.Members.", "Email", "/mutatorqueries/orm.go", []string{"MutateEmail", "not during assignment construction", "delivery address."}},
		{"config-group", "startupconfig/settings_test.go", "Overrides: []config.Override[startupconfig.Settings]{keys.", "HTTP", "/startupconfig/settings_foundry.gen.go", []string{"public listener", "request timeout"}},
		{"config-trailing-field", "startupconfig/settings_test.go", "keys.HTTP.", "Port", "/startupconfig/settings_foundry.gen.go", []string{"public listener port", "http.port", "Port"}},
		{"imported-config-field", "bootstrap/bootstrap_test.go", "keys.App.Features.Outbox.", "Events", "/bootstrap/settings_foundry.gen.go", []string{"EventOutbox", "durable job connections", "Empty connection selects the default", "jobs.DefaultPolicy"}},
		{"validation-field", "clientcontracts/validation_fields_test.go", "clientcontracts.PayloadValidationFields().", "Titles", "/clientcontracts/payload_foundry.gen.go", []string{"keyed by the catalog's supported locales", "titles"}},
		{"projection-selection", "projectionqueries/projections_postgres_test.go", "rows.Map(reports.UserSummaries.", "Email", "/reports/rows.go", []string{"buyer contact address selected for this report"}},
		{"orm-row-selection", "reporting/members.go", "rows.MapSelected(MemberRows.", "Name", "/reporting/rows.go", []string{"Display name selected for the member report"}},
		{"extension-descriptor", "articles/loading_postgres_test.go", "s.x.", "Title", "/articles/tables.go", []string{"localized article heading shown to readers", "keeps its locale input and owner", "ArticleSlots.Title.Rule"}},
		{"changes-field", "mutatorqueries/changes_postgres_test.go", "orm.ChangeOf(changes, q.", "Email", "/mutatorqueries/orm.go", []string{"delivery address.", "stored values", "MutateEmail"}},
		{"json-property", "jsonqueries/path_postgres_test.go", "expr.JSONProperty(prefs, jsonqueries.", "Theme", "/jsonqueries/tables.go", []string{"reader appearance", "omitted uses the application default"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			parallelRealGopls(t)
			path := filepath.Join(workspace, probe.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(data)
			start := strings.Index(source, probe.prefix+probe.symbol)
			if start < 0 {
				t.Fatal("field inspection expression was removed")
			}
			var operations []Options
			for _, operation := range []string{"complete", "hover", "definition"} {
				offset := start + len(probe.prefix)
				if operation != "complete" {
					offset++
				}
				operations = append(operations, Options{Workspace: workspace, File: path, Operation: operation, Gopls: executable, Line: strings.Count(source[:offset], "\n") + 1, Column: offset - strings.LastIndexByte(source[:offset], '\n')})
			}
			results, err := InspectBatch(t.Context(), operations, 45*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(operations) {
				t.Fatal("incomplete field documentation scenario")
			}
			for _, result := range results {
				operation := result.Operation
				if operation == "definition" {
					if !strings.Contains(string(result.Payload), probe.definition) {
						t.Fatalf("wrong field definition: %s", result.Payload)
					}
					continue
				}
				var hover struct{ Contents json.RawMessage }
				var doc string
				if operation == "complete" {
					var list struct {
						Items []struct {
							Label         string
							Documentation json.RawMessage
						} `json:"items"`
					}
					if err := json.Unmarshal(result.Payload, &list); err != nil {
						if err := json.Unmarshal(result.Payload, &list.Items); err != nil {
							t.Fatal(err)
						}
					}
					doc = ""
					for _, item := range list.Items {
						if item.Label == probe.symbol || strings.HasPrefix(item.Label, probe.symbol+"(") {
							doc, err = completionDocumentation(item.Documentation)
							if err != nil {
								t.Fatal(err)
							}
							break
						}
					}
					if doc == "" {
						t.Fatalf("gopls completion omitted documentation for %s", probe.symbol)
					}
				} else {
					if err := json.Unmarshal(result.Payload, &hover); err != nil {
						t.Fatal(err)
					}
					doc, err = completionDocumentation(hover.Contents)
					if err != nil {
						t.Fatal(err)
					}
				}
				// Markdown punctuation escaping does not change the displayed
				// identifier. Keep the actual LSP response unmodified.
				display := strings.NewReplacer("\\_", "_", "\\[", "[", "\\]", "]")
				doc = display.Replace(doc)
				for _, want := range probe.documentation {
					if !strings.Contains(doc, want) {
						t.Fatalf("%s lost %q: %s", operation, want, doc)
					}
				}
				var readable bytes.Buffer
				if err := WriteText(&readable, result); err != nil {
					t.Fatal(err)
				}
				for _, want := range probe.documentation {
					if !strings.Contains(display.Replace(readable.String()), want) {
						t.Fatalf("readable %s lost %q", operation, want)
					}
				}
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, data) {
				t.Fatal("language inspection changed consumer source")
			}
		})
	}
}

func TestRealGoplsStoredFieldCapabilities(t *testing.T) {
	testkit.TrackExternalInputs(t)
	executable := os.Getenv("FOUNDRY_TEST_GOPLS")
	if executable == "" {
		t.Skip("requires the existing approved gopls executable")
	}
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		var err error
		executable, err = filepath.Abs(executable)
		if err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := filepath.Abs("../../tests/fixtures/consumer")
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		name, file, prefix, symbol string
		methods, forbidden         []string
	}{
		{"text", "mutatorqueries/mutators_postgres_test.go", "q.Query().Where(f.Email.", "Eq", []string{"Eq", "Like", "Contains", "Set"}, nil},
		{"numeric", "mutatorqueries/mutators_postgres_test.go", "mutatorqueries.Members.Attempts.", "Set", []string{"Eq", "Gte", "Set", "Asc"}, nil},
		{"nullable-clearing", "generated_test.go", "fields.Nickname.", "SetNull", []string{"Set", "SetNull"}, nil},
		{"required-no-clearing", "generated_test.go", "fields.Email.", "Set", []string{"Set"}, []string{"SetNull"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			parallelRealGopls(t)
			path := filepath.Join(workspace, probe.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(data)
			start := strings.Index(source, probe.prefix+probe.symbol)
			if start < 0 {
				t.Fatal("stored-field inspection expression was removed")
			}
			offset := start + len(probe.prefix)
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			result, err := Inspect(ctx, Options{Workspace: workspace, File: path, Operation: "complete", Gopls: executable, Line: strings.Count(source[:offset], "\n") + 1, Column: offset - strings.LastIndexByte(source[:offset], '\n')})
			if err != nil {
				t.Fatal(err)
			}
			var list struct{ Items []struct{ Label string } }
			if err := json.Unmarshal(result.Payload, &list); err != nil {
				if err := json.Unmarshal(result.Payload, &list.Items); err != nil {
					t.Fatal(err)
				}
			}
			labels := make(map[string]bool)
			for _, item := range list.Items {
				labels[strings.SplitN(item.Label, "(", 2)[0]] = true
			}
			for _, want := range probe.methods {
				if !labels[want] {
					t.Fatalf("gopls lost stored-field method %s: %s", want, result.Payload)
				}
			}
			for _, removed := range append([]string{"Value", "Param", "Count", "CountDistinct", "Min", "Max", "Sum", "Avg"}, probe.forbidden...) {
				if labels[removed] {
					t.Fatalf("gopls suggested removed stored-field method %s", removed)
				}
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, current) {
				t.Fatal("stored-field inspection changed consumer source")
			}
		})
	}
}
