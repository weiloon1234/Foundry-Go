package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
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
	type fieldDocProbe struct {
		name, file, prefix, symbol, definition string
		documentation                          []string
	}
	probes := []fieldDocProbe{
		{"sensitive-password-field", "passwords/account_postgres_test.go", "return account.", "Digest", "/passwords/account.go", []string{"Sensitive stored password hash", "password.Codec", "redacted", "Compare-and-swap"}},
		{"raw-field-both", "mutatorqueries/accessors_test.go", "storedEmail := member.", "Email", "/mutatorqueries/member.go", []string{"Email is the delivery address stored in email_address", "Call AccessEmail explicitly", "MutateEmail runs during persistence through orm.InputTextSet(Members.Email", "assigning this field does not invoke it"}},
		{"raw-field-getter", "mutatorqueries/accessors_test.go", "if member.", "ID", "/mutatorqueries/member.go", []string{"ID is the database identity", "Stored reads keep the key", "call AccessID explicitly for presentation"}},
		{"raw-field-setter", "mutatorqueries/accessors_test.go", "|| member.", "Attempts", "/mutatorqueries/member.go", []string{"Attempts stores the persisted count", "MutateAttempts runs during persistence through orm.InputNumberSet(Members.Attempts", "Direct assignment"}},
		{"query-field", "mutatorqueries/mutators_postgres_test.go", "q.Query().Where(orm.InputTextEq(f.", "Email", "/mutatorqueries/orm.go", []string{"Custom getter", "AccessEmail", "Custom setter", "MutateEmail", "delivery address."}},
		{"extension-slot-field", "articles/cleanup_postgres_test.go", "if article.", "Title", "/articles/article.go", []string{"Extension slot, not a column", "foundry_model_translations", "ArticleSlots.Title", "From(runtime)", "localized article heading shown to readers"}},
		{"draft-setter", "mutatorqueries/mutators_postgres_test.go", "mutatorqueries.Members.", "Email", "/mutatorqueries/orm.go", []string{"MutateEmail", "not during assignment construction", "delivery address."}},
		{"config-group", "startupconfig/settings_test.go", "Overrides: []config.Override[startupconfig.Settings]{keys.", "HTTP", "/startupconfig/settings_foundry.gen.go", []string{"public listener", "request timeout"}},
		{"config-trailing-field", "startupconfig/settings_test.go", "keys.HTTP.", "Port", "/startupconfig/settings_foundry.gen.go", []string{"public listener port", "http.port", "Port"}},
		{"imported-config-field", "bootstrap/bootstrap_test.go", "keys.App.Features.Outbox.", "Events", "/bootstrap/settings_foundry.gen.go", []string{"EventOutbox", "durable job connections", "Empty connection selects the default", "jobs.DefaultPolicy"}},
		{"validation-field", "clientcontracts/validation_fields_test.go", "clientcontracts.PayloadValidationFields().", "Titles", "/clientcontracts/payload_foundry.gen.go", []string{"keyed by the catalog's supported locales", "titles"}},
		{"projection-selection", "projectionqueries/projections_postgres_test.go", "rows.Map(reports.UserSummaries.", "Email", "/reports/rows.go", []string{"buyer contact address selected for this report"}},
		{"orm-row-selection", "reporting/members.go", "rows.MapSelected(MemberRows.", "Name", "/reporting/rows.go", []string{"Display name selected for the member report"}},
		{"extension-descriptor", "articles/loading_postgres_test.go", "s.x.", "Title", "/articles/tables.go", []string{"localized article heading shown to readers", "keeps its locale input and owner", "ArticleSlots.Title.Rule"}},
		{"changes-field", "mutatorqueries/changes_postgres_test.go", "orm.ChangeOf(changes, orm.InputTextExpr(q.", "Email", "/mutatorqueries/orm.go", []string{"delivery address.", "stored values", "MutateEmail"}},
		{"json-property", "jsonqueries/path_postgres_test.go", "expr.JSONProperty(prefs, jsonqueries.", "Theme", "/jsonqueries/tables.go", []string{"reader appearance", "omitted uses the application default"}},
	}

	type fieldDocChunk struct {
		probes    []fieldDocProbe
		once      sync.Once
		results   []Result
		err       error
		sourceErr error
	}

	runFieldDocChunk := func(c *fieldDocChunk, ctx context.Context) ([]Result, error, error) {
		c.once.Do(func() {
			select {
			case realGoplsSlots <- struct{}{}:
				defer func() { <-realGoplsSlots }()
			case <-ctx.Done():
				c.err = ctx.Err()
				return
			}
			var operations []Options
			for _, probe := range c.probes {
				path := filepath.Join(workspace, probe.file)
				data, err := os.ReadFile(path)
				if err != nil {
					c.err = err
					return
				}
				source := string(data)
				start := strings.Index(source, probe.prefix+probe.symbol)
				if start < 0 {
					c.err = fmt.Errorf("field inspection expression was removed for probe %s", probe.name)
					return
				}
				for _, operation := range []string{"complete", "hover", "definition"} {
					offset := start + len(probe.prefix)
					if operation != "complete" {
						offset++
					}
					operations = append(operations, Options{
						Workspace: workspace,
						File:      path,
						Operation: operation,
						Gopls:     executable,
						Line:      strings.Count(source[:offset], "\n") + 1,
						Column:    offset - strings.LastIndexByte(source[:offset], '\n'),
					})
				}
			}
			c.results, c.err, c.sourceErr = inspectConsumerEditorBatch(operations, func() ([]Result, error) {
				return InspectBatch(ctx, operations, 45*time.Second)
			})
		})
		return c.results, c.err, c.sourceErr
	}

	const fieldDocChunkLimit = 5
	for chunkStart := 0; chunkStart < len(probes); chunkStart += fieldDocChunkLimit {
		chunkEnd := min(chunkStart+fieldDocChunkLimit, len(probes))
		chunk := &fieldDocChunk{probes: probes[chunkStart:chunkEnd]}
		for pIdx, probe := range chunk.probes {
			pIdx := pIdx
			probe := probe
			t.Run(probe.name, func(t *testing.T) {
				t.Parallel()
				results, batchErr, sourceErr := runFieldDocChunk(chunk, t.Context())
				if sourceErr != nil {
					t.Error(sourceErr)
				}
				startOp := pIdx * 3
				endOp := startOp + 3
				if batchErr != nil {
					var failedOp int
					var opName string
					if n, _ := fmt.Sscanf(batchErr.Error(), "agent batch operation %d (%s):", &failedOp, &opName); n >= 1 {
						if failedOp > startOp && failedOp <= endOp {
							t.Fatalf("agent batch operation failed: %v", batchErr)
						} else if failedOp <= startOp {
							t.Fatalf("prior operation failed in agent batch: %v", batchErr)
						}
					} else {
						t.Fatalf("agent batch failed: %v", batchErr)
					}
				}
				if len(results) < endOp {
					t.Fatalf("incomplete field documentation scenario: got %d results, need at least %d", len(results), endOp)
				}
				var err error
				for _, result := range results[startOp:endOp] {
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
			})
		}
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
		name, file, prefix, symbol, insert string
		functions, forbidden               []string
	}{
		{"text-methodless", "mutatorqueries/mutators_postgres_test.go", "orm.InputTextEq(f.Email", ",", ".", nil, nil},
		{"numeric-methodless", "mutatorqueries/mutators_postgres_test.go", "orm.InputNumberSet(mutatorqueries.Members.Attempts", ",", ".", nil, nil},
		{"nullable-methodless", "generated_test.go", "orm.NullableTextSetNull(fields.Nickname", ")", ".", nil, nil},
		{"required-methodless", "generated_test.go", "orm.TextSet(fields.Email", ",", ".", nil, nil},
		{"text-functions", "mutatorqueries/mutators_postgres_test.go", "orm.", "InputTextEq", "", []string{"InputTextEq", "InputTextLike", "InputTextContains", "InputTextSet"}, []string{"InputTextSetNull"}},
		{"numeric-functions", "mutatorqueries/mutators_postgres_test.go", "orm.", "InputNumberSet", "", []string{"InputNumberEq", "InputNumberGte", "InputNumberSet", "InputNumberAsc"}, []string{"InputNumberLike"}},
		{"nullable-clearing", "generated_test.go", "orm.", "NullableTextSetNull", "", []string{"NullableTextSet", "NullableTextSetNull"}, nil},
		{"required-no-clearing", "generated_test.go", "orm.", "TextSet", "", []string{"TextSet"}, []string{"TextSetNull"}},
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
			result, err := Inspect(ctx, Options{Workspace: workspace, File: path, Operation: "complete", Gopls: executable, Insert: probe.insert, Line: strings.Count(source[:offset], "\n") + 1, Column: offset - strings.LastIndexByte(source[:offset], '\n')})
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
			if probe.insert == "." && len(labels) != 0 {
				t.Fatalf("gopls suggested members on a methodless column: %s", result.Payload)
			}
			for _, want := range probe.functions {
				if !labels[want] {
					t.Fatalf("gopls lost stored-field function %s: %s", want, result.Payload)
				}
			}
			for _, removed := range probe.forbidden {
				if labels[removed] {
					t.Fatalf("gopls suggested unsupported stored-field function %s", removed)
				}
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, current) {
				t.Fatal("stored-field inspection changed consumer source")
			}
		})
	}
}
