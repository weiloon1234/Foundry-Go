package retirement

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReplacementHeadsResolve(t *testing.T) {
	entries, err := retiredDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{"database": "database", "value": "value"}
	for owner := range entries {
		owners[path.Base(owner)] = owner
	}
	symbols := make(map[string]map[string]bool)
	heads := regexp.MustCompile(`\b([a-z][a-z0-9]*)\.([A-Z][A-Za-z0-9_]*)`)
	for owner, members := range entries {
		seen := make(map[string]bool)
		for _, member := range members {
			if member.Name == "" || seen[member.Name] {
				t.Fatalf("invalid or duplicate ledger entry %s.%s", owner, member.Name)
			}
			seen[member.Name] = true
			for _, head := range heads.FindAllStringSubmatch(member.Replacement, -1) {
				pkg, known := owners[head[1]]
				if !known {
					t.Errorf("%s.%s: unknown replacement package %s", owner, member.Name, head[1])
					continue
				}
				if symbols[pkg] == nil {
					symbols[pkg] = make(map[string]bool)
					dir := filepath.Join("../..", pkg)
					files, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range files {
						if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
							continue
						}
						file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
						if err != nil {
							t.Fatal(err)
						}
						for _, decl := range file.Decls {
							switch d := decl.(type) {
							case *ast.FuncDecl:
								if d.Recv == nil {
									symbols[pkg][d.Name.Name] = true
								}
							case *ast.GenDecl:
								for _, spec := range d.Specs {
									switch s := spec.(type) {
									case *ast.TypeSpec:
										symbols[pkg][s.Name.Name] = true
									case *ast.ValueSpec:
										for _, name := range s.Names {
											symbols[pkg][name.Name] = true
										}
									}
								}
							}
						}
					}
				}
				if !symbols[pkg][head[2]] {
					t.Errorf("%s.%s: replacement %s does not exist in %s", owner, member.Name, head[0], pkg)
				}
			}
		}
	}
}

func TestRetirementReplacementDiagnostics(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"orm.DoesntHave", "orm.DoesntHave is retired; use orm.Not(orm.Has(relation, predicates...))."},
		{"attachments.AddFiles", "attachments.AddFiles is retired; use attachments.AddFilesTo."},
		{"orm.DeclarationSnapshot", "orm.DeclarationSnapshot is retired; no replacement is recorded; remove this use and consult the current task guide."},
		{"orm.Predicate.Not", "orm.Predicate.Not is retired; use orm.Not(predicate)."},
	} {
		err := CheckDocument("guide.md", []byte("# Guide\n\nUse `"+test.code+"`.\n"))
		if err == nil || !strings.Contains(err.Error(), "guide.md:3: "+test.want) {
			t.Fatalf("%s: %v", test.code, err)
		}
	}
}

func TestReplacementLedgerPreservesNameConsumers(t *testing.T) {
	// The fixed set is the denied-name ledger at 7b364318, before replacement metadata.
	expected := strings.Fields(`application.WithErrorReporter
application.WithLogHandler
application.WithTraceBatchExporter
application.WithTraceExporter
attachments.AddFiles
attachments.Collection.Matching
attachments.DefineMany
attachments.DefineOne
attachments.ManySlot
attachments.ManySlot.Add
attachments.ManySlot.AddFile
attachments.ManySlot.AddIn
attachments.ManySlot.Format
attachments.ManySlot.From
attachments.ManySlot.PublicURLs
attachments.ManySlot.Reorder
attachments.OneSlot
attachments.OneSlot.Format
attachments.OneSlot.From
attachments.OneSlot.PublicURL
audit.ParseModelID
database/orm.DeclarationOwner
database/orm.DeclarationSlot
database/orm.DeclarationSnapshot
database/orm.DoesntHave
database/orm.EnumDesc
database/orm.Extension
database/orm.FoundryCompareDeclarationSnapshots
database/orm.FoundryExplainDeclarationSnapshots
database/orm.InputColumnDesc
database/orm.InputColumnNotIn
database/orm.InputEnumAsc
database/orm.InputEnumDesc
database/orm.InputEnumIn
database/orm.InputEnumNe
database/orm.InputEnumNotIn
database/orm.InputIntervalAsc
database/orm.InputIntervalBetween
database/orm.InputIntervalDesc
database/orm.InputIntervalEq
database/orm.InputIntervalGt
database/orm.InputIntervalGte
database/orm.InputIntervalIn
database/orm.InputIntervalLt
database/orm.InputIntervalLte
database/orm.InputIntervalNe
database/orm.InputIntervalNotIn
database/orm.InputJSONAsc
database/orm.InputJSONDesc
database/orm.InputJSONEq
database/orm.InputJSONIn
database/orm.InputJSONNe
database/orm.InputJSONNotIn
database/orm.InputKeyAsc
database/orm.InputKeyDesc
database/orm.InputKeyIn
database/orm.InputKeyNe
database/orm.InputKeyNotIn
database/orm.InputNumberBetween
database/orm.InputNumberDesc
database/orm.InputNumberGt
database/orm.InputNumberIn
database/orm.InputNumberLt
database/orm.InputNumberLte
database/orm.InputNumberNe
database/orm.InputNumberNotIn
database/orm.InputTextBetween
database/orm.InputTextDesc
database/orm.InputTextEndsWith
database/orm.InputTextGt
database/orm.InputTextGte
database/orm.InputTextIContains
database/orm.InputTextIEndsWith
database/orm.InputTextILike
database/orm.InputTextIStartsWith
database/orm.InputTextIn
database/orm.InputTextLt
database/orm.InputTextLte
database/orm.InputTextNotIn
database/orm.InputTextStartsWith
database/orm.InputTimeAsc
database/orm.InputTimeBetween
database/orm.InputTimeDesc
database/orm.InputTimeEq
database/orm.InputTimeGt
database/orm.InputTimeGte
database/orm.InputTimeIn
database/orm.InputTimeLt
database/orm.InputTimeLte
database/orm.InputTimeNe
database/orm.InputTimeNotIn
database/orm.KeyColumnEq
database/orm.LegacyIdentityScope
database/orm.Load
database/orm.LoadMissing
database/orm.MorphName
database/orm.Morphable
database/orm.Mutate
database/orm.NullableEnumAsc
database/orm.NullableEnumDesc
database/orm.NullableEnumIn
database/orm.NullableEnumNotIn
database/orm.NullableInputColumnAsc
database/orm.NullableInputColumnDesc
database/orm.NullableInputColumnEq
database/orm.NullableInputColumnIn
database/orm.NullableInputColumnIsNotNull
database/orm.NullableInputColumnNe
database/orm.NullableInputColumnNotIn
database/orm.NullableInputEnumAsc
database/orm.NullableInputEnumDesc
database/orm.NullableInputEnumEq
database/orm.NullableInputEnumIn
database/orm.NullableInputEnumIsNotNull
database/orm.NullableInputEnumIsNull
database/orm.NullableInputEnumNe
database/orm.NullableInputEnumNotIn
database/orm.NullableInputIntervalAsc
database/orm.NullableInputIntervalBetween
database/orm.NullableInputIntervalDesc
database/orm.NullableInputIntervalEq
database/orm.NullableInputIntervalGt
database/orm.NullableInputIntervalGte
database/orm.NullableInputIntervalIn
database/orm.NullableInputIntervalIsNotNull
database/orm.NullableInputIntervalIsNull
database/orm.NullableInputIntervalLt
database/orm.NullableInputIntervalLte
database/orm.NullableInputIntervalNe
database/orm.NullableInputIntervalNotIn
database/orm.NullableInputJSONAsc
database/orm.NullableInputJSONDesc
database/orm.NullableInputJSONEq
database/orm.NullableInputJSONIn
database/orm.NullableInputJSONIsNotNull
database/orm.NullableInputJSONIsNull
database/orm.NullableInputJSONNe
database/orm.NullableInputJSONNotIn
database/orm.NullableInputNumberAsc
database/orm.NullableInputNumberBetween
database/orm.NullableInputNumberDesc
database/orm.NullableInputNumberEq
database/orm.NullableInputNumberGt
database/orm.NullableInputNumberGte
database/orm.NullableInputNumberIn
database/orm.NullableInputNumberIsNotNull
database/orm.NullableInputNumberIsNull
database/orm.NullableInputNumberLt
database/orm.NullableInputNumberLte
database/orm.NullableInputNumberNe
database/orm.NullableInputNumberNotIn
database/orm.NullableInputTextAsc
database/orm.NullableInputTextBetween
database/orm.NullableInputTextDesc
database/orm.NullableInputTextEndsWith
database/orm.NullableInputTextEq
database/orm.NullableInputTextGt
database/orm.NullableInputTextGte
database/orm.NullableInputTextIContains
database/orm.NullableInputTextIEndsWith
database/orm.NullableInputTextILike
database/orm.NullableInputTextIStartsWith
database/orm.NullableInputTextIn
database/orm.NullableInputTextIsNotNull
database/orm.NullableInputTextIsNull
database/orm.NullableInputTextLike
database/orm.NullableInputTextLt
database/orm.NullableInputTextLte
database/orm.NullableInputTextNe
database/orm.NullableInputTextNotIn
database/orm.NullableInputTextStartsWith
database/orm.NullableInputTimeAsc
database/orm.NullableInputTimeBetween
database/orm.NullableInputTimeDesc
database/orm.NullableInputTimeEq
database/orm.NullableInputTimeGt
database/orm.NullableInputTimeGte
database/orm.NullableInputTimeIn
database/orm.NullableInputTimeIsNotNull
database/orm.NullableInputTimeIsNull
database/orm.NullableInputTimeLt
database/orm.NullableInputTimeLte
database/orm.NullableInputTimeNe
database/orm.NullableInputTimeNotIn
database/orm.NullableNumberBetween
database/orm.NullableNumberIn
database/orm.NullableNumberLt
database/orm.NullableNumberLte
database/orm.NullableNumberNe
database/orm.NullableNumberNotIn
database/orm.NullableTextBetween
database/orm.NullableTextGt
database/orm.NullableTextGte
database/orm.NullableTextLt
database/orm.NullableTextLte
database/orm.NullableTextNotIn
database/orm.NullableTimeBetween
database/orm.NullableTimeEq
database/orm.NullableTimeIn
database/orm.NullableTimeNe
database/orm.NullableTimeNotIn
database/orm.NumberKeyDesc
database/orm.NumberKeyIn
database/orm.NumberKeyNe
database/orm.NumberKeyNotIn
database/orm.Predicate.Not
database/orm.Query.ForUpdate
database/orm.TextBetween
database/orm.TextGte
database/orm.TextLt
database/orm.TextLte
database/orm.TimeNe
database/orm.TimeNotIn
database/orm/expr.JSONArray
database/orm/expr.JSONBoolean
database/orm/expr.JSONKind
database/orm/expr.JSONNull
database/orm/expr.JSONNumber
database/orm/expr.JSONObject
database/orm/expr.JSONOrderedScalar
database/orm/expr.JSONPath.Contains
database/orm/expr.JSONPath.Exists
database/orm/expr.JSONPath.IsJSONNull
database/orm/expr.JSONPath.IsMissing
database/orm/expr.JSONPath.Kind
database/orm/expr.JSONPath.Length
database/orm/expr.JSONString
database/orm/expr.NullFor
database/orm/expr.Select
database/orm/rows.Compile
database/orm/rows.CorrelatedScalarNullableQuery
database/orm/rows.CorrelatedScalarQuery
database/orm/rows.Count
database/orm/rows.Each
database/orm/rows.ExplainAnalyzeLocked
database/orm/rows.ExplainLocked
database/orm/rows.Paginate
database/orm/rows.Preflight
database/orm/rows.Query.All
database/orm/rows.Query.OrderBy
database/orm/rows.RecordRead.All
database/orm/rows.ScalarNullableQuery
database/orm/rows.ScalarQuery
database/orm/rows.SetQuery.All
database/orm/rows.SimplePaginate
database/orm/rows.TransactionCorrelatedScalarNullableQuery
database/orm/rows.TransactionCorrelatedScalarQuery
database/orm/rows.TransactionScalarNullableQuery
database/orm/rows.TransactionScalarQuery
database/orm/rows.ValueCursorQuery.Key
database/orm/rows.ValueSetQuery.All
database/prune.Model
datatable.Cell.FormatWith
datatable.Config.DownloadTimeout
deployment.Bindings.DeploymentSHA256
extensions.DefineOwner
extensions.DefineOwnerWith
extensions.Owner.Active
extensions.Owner.ModelName
extensions.Owner.QueryScope
extensions.Owner.Reference
extensions.Scope
http.AuthenticatedEndpoint.WithAuthorization
http.AuthenticatedEndpoint.WithBodyValidation
http.AuthenticatedEndpoint.WithMiddleware
http.AuthenticatedEndpoint.WithPermissions
http.AuthenticatedEndpoint.WithScopes
http.AuthenticatedIdempotentEndpoint.WithAuthorization
http.AuthenticatedIdempotentEndpoint.WithBodyValidation
http.AuthenticatedIdempotentEndpoint.WithMiddleware
http.AuthenticatedIdempotentEndpoint.WithPermissions
http.AuthenticatedIdempotentEndpoint.WithScopes
http.CSPReportSample
http.CSPSHA384
http.CSPSHA512
http.CSPUnsafeEval
http.CSPUnsafeHashes
http.Endpoint.WithAuthorization
http.Endpoint.WithBodyValidation
http.Endpoint.WithMiddleware
http.Endpoint.WithPermissions
http.Endpoint.WithScopes
http.Events.WithConfig
http.FloatCookie
http.IdempotentEndpoint.WithAuthorization
http.IdempotentEndpoint.WithBodyValidation
http.IdempotentEndpoint.WithMiddleware
http.IdempotentEndpoint.WithPermissions
http.IdempotentEndpoint.WithScopes
http.OptionalAuthenticationEndpoint.WithAuthorization
http.OptionalAuthenticationEndpoint.WithBodyValidation
http.OptionalAuthenticationEndpoint.WithMiddleware
http.OptionalAuthenticationEndpoint.WithPermissions
http.OptionalAuthenticationEndpoint.WithScopes
http.OriginPattern.Wildcard
http.SignedAuthenticatedEndpoint.WithAuthorization
http.SignedAuthenticatedEndpoint.WithBodyValidation
http.SignedAuthenticatedEndpoint.WithMiddleware
http.SignedAuthenticatedEndpoint.WithPermissions
http.SignedAuthenticatedEndpoint.WithScopes
http.SignedEndpoint.WithAuthorization
http.SignedEndpoint.WithBodyValidation
http.SignedEndpoint.WithMiddleware
http.SignedEndpoint.WithPermissions
http.SignedEndpoint.WithScopes
http.TicketEndpoint.WithAuthorization
http.TicketEndpoint.WithBodyValidation
http.TicketEndpoint.WithMiddleware
http.TicketEndpoint.WithPermissions
http.TicketEndpoint.WithScopes
http/modelbinding.AuthenticatedEndpoint.WithAuthorization
http/modelbinding.AuthenticatedEndpoint.WithBodyValidation
http/modelbinding.AuthenticatedEndpoint.WithMiddleware
http/modelbinding.AuthenticatedEndpoint.WithPermissions
http/modelbinding.AuthenticatedEndpoint.WithScopes
http/modelbinding.AuthenticatedIdempotentEndpoint.WithAuthorization
http/modelbinding.AuthenticatedIdempotentEndpoint.WithBodyValidation
http/modelbinding.AuthenticatedIdempotentEndpoint.WithMiddleware
http/modelbinding.AuthenticatedIdempotentEndpoint.WithPermissions
http/modelbinding.AuthenticatedIdempotentEndpoint.WithScopes
http/modelbinding.ByField
http/modelbinding.Endpoint.WithAuthorization
http/modelbinding.Endpoint.WithBodyValidation
http/modelbinding.Endpoint.WithMiddleware
http/modelbinding.Endpoint.WithPermissions
http/modelbinding.Endpoint.WithScopes
http/modelbinding.IdempotentEndpoint.WithAuthorization
http/modelbinding.IdempotentEndpoint.WithBodyValidation
http/modelbinding.IdempotentEndpoint.WithMiddleware
http/modelbinding.IdempotentEndpoint.WithPermissions
http/modelbinding.IdempotentEndpoint.WithScopes
http/modelbinding.Through
http/modelbinding.ThroughSelected
http/pagination.AuthenticatedCursorEndpoint.WithAuthorization
http/pagination.AuthenticatedCursorEndpoint.WithPermissions
http/pagination.AuthenticatedCursorEndpoint.WithScopes
http/pagination.AuthenticatedNumberedEndpoint.WithAuthorization
http/pagination.AuthenticatedNumberedEndpoint.WithPermissions
http/pagination.AuthenticatedNumberedEndpoint.WithScopes
http/pagination.AuthenticatedSimpleEndpoint.WithAuthorization
http/pagination.AuthenticatedSimpleEndpoint.WithPermissions
http/pagination.AuthenticatedSimpleEndpoint.WithScopes
http/pagination.authenticatedPageEndpoint.WithAuthorization
http/pagination.authenticatedPageEndpoint.WithPermissions
http/pagination.authenticatedPageEndpoint.WithScopes
imaging.Format.CanDecode
jobs.DefaultOverlapOptions
jobs.LegacyEnvelope
metadata.DefineValue
metadata.Key.Matching
metadata.ValueSlot
metadata.ValueSlot.Describe
metadata.ValueSlot.Forget
metadata.ValueSlot.ForgetIn
metadata.ValueSlot.Format
metadata.ValueSlot.From
metadata.ValueSlot.Key
metadata.ValueSlot.Name
metadata.ValueSlot.Registration
metadata.ValueSlot.Save
metadata.ValueSlot.SaveIn
metadata.ValueSlot.Validate
model.NewID
redis.JobKeys
redis/data.AllScores
redis/raw.DecodeBytes
schedule.Weekends
storage.DeletePrefixOptions
storage.DeletePrefixResult
storage.Disk.DeletePrefix
testkit/factory.Model
testkit/factory.New
translations.DefineText
translations.Field.Matching
translations.TextSlot
translations.TextSlot.Clear
translations.TextSlot.ClearIn
translations.TextSlot.Describe
translations.TextSlot.Field
translations.TextSlot.Forget
translations.TextSlot.ForgetIn
translations.TextSlot.Format
translations.TextSlot.From
translations.TextSlot.MergeRule
translations.TextSlot.Name
translations.TextSlot.Options
translations.TextSlot.Registration
translations.TextSlot.Rule
translations.TextSlot.Save
translations.TextSlot.SaveIn
translations.TextSlot.Sync
translations.TextSlot.SyncIn
translations.TextSlot.Validate
validation.AfterField
validation.AfterOrEqualNow
validation.BeforeOrEqualField
validation.Field.Rules
validation.Rule.Check`)
	names, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	actual := make(map[string]bool)
	for owner, members := range names {
		for _, member := range members {
			actual[owner+"."+member] = true
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("denied names = %d, baseline = %d", len(actual), len(expected))
	}
	for _, name := range expected {
		if !actual[name] {
			t.Errorf("lost denial %s", name)
		}
	}
}

func TestUnqualifiedRetirementKeepsCodeLine(t *testing.T) {
	err := CheckDocument("guide.md", []byte("# Guide\n\n```go\nvar current = 1\n\nDoesntHave(relation)\n```\n"))
	if err == nil || !strings.Contains(err.Error(), "guide.md:6: `DoesntHave` matches the retired `orm.DoesntHave`; write the qualified name you mean") {
		t.Fatalf("retirement lost its source line: %v", err)
	}
}

func TestReplacementReportingRespectsExternalOwners(t *testing.T) {
	for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
		for _, alias := range []string{"validation", "v"} {
			for _, owner := range []string{"example.test/validation", framework + "validation"} {
				code := "```go\npackage p\nimport " + alias + " \"" + owner + "\"\nvar check = " + alias + ".Rule.Check\n```\n"
				err := check("AGENTS.md", []byte(code))
				if owner == "example.test/validation" {
					if err != nil {
						t.Fatalf("external owner rejected with alias %s: %v", alias, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "validation.Rule.Check is retired;") {
					t.Fatalf("framework owner missed with alias %s: %v", alias, err)
				}
			}
		}
	}
}

func TestNestedPackageReplacementAliases(t *testing.T) {
	for _, test := range []struct{ owner, alias, member, replacement string }{
		{"testkit/factory", "f", "New", "factory.Define"},
		{"database/prune", "p", "Model", "prune.ORMModel or prune.ORMSoftModel"},
	} {
		for _, alias := range []string{test.alias, path.Base(test.owner), "."} {
			expression := alias + "." + test.member
			if alias == "." {
				expression = test.member
			}
			code := "```go\npackage p\nimport " + alias + " \"" + framework + test.owner + "\"\nvar use = " + expression + "\n```\n"
			err := CheckApplicationDocument("AGENTS.md", []byte(code))
			if err == nil || !strings.Contains(err.Error(), "AGENTS.md:4: "+path.Base(test.owner)+"."+test.member+" is retired; use "+test.replacement+".") || strings.Contains(err.Error(), "no replacement") {
				t.Fatalf("lost replacement through %s: %v", alias, err)
			}
		}
		err := CheckDocument("guide.md", []byte("Use `"+path.Base(test.owner)+"."+test.member+"`.\n"))
		if err == nil || !strings.Contains(err.Error(), "use "+test.replacement+".") {
			t.Fatalf("lost direct replacement: %v", err)
		}
	}
}

func TestQuotedGoImportsRetainOwnership(t *testing.T) {
	for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
		for _, owner := range []string{"example.test/validation", "github.com/weiloon1234/Foundry-Go/validation"} {
			input := "> ```go\n> package p\n> import validation \"" + owner + "\"\n> var check = validation.Rule.Check\n> ```\n"
			err := check("guide.md", []byte(input))
			if strings.HasPrefix(owner, "example.test") {
				if err != nil {
					t.Fatalf("external owner rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "guide.md:4:") {
				t.Fatalf("framework owner escaped: %v", err)
			}
		}
	}
}
