package retirement

import (
	"fmt"
	"strings"
	"testing"
)

func TestPaginationDocumentationOwners(t *testing.T) {
	for _, owner := range []string{"AuthenticatedNumberedEndpoint[P,F,A,T]", "AuthenticatedSimpleEndpoint[P,F,A,T]", "AuthenticatedCursorEndpoint[P,F,M,A,T]"} {
		for _, method := range []string{"WithScopes", "WithPermissions", "WithAuthorization"} {
			for _, imported := range []string{"github.com/weiloon1234/Foundry-Go/http/pagination", "example.test/pagination"} {
				code := fmt.Sprintf("package example\nimport pages %q\nfunc use(e pages.%s) { e.%s(option) }", imported, owner, method)
				err := CheckDocument("AGENTS.md", []byte("```go\n"+code+"\n```\n"))
				if strings.HasPrefix(imported, "example.test/") {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "retired API pagination.") || strings.Contains(err.Error(), "ambiguous") {
					t.Fatalf("%s: %v", code, err)
				}
			}
		}
	}
	for _, code := range []string{
		"e := pagination.Authenticated(page, binding)\ne.WithScopes(scopes)",
		"package example\nimport p \"github.com/weiloon1234/Foundry-Go/http/pagination\"\nfunc use() { e := p.Authenticated(page, binding); e.WithAuthorization(policy) }",
	} {
		if err := CheckDocument("AGENTS.md", []byte("```go\n"+code+"\n```")); err == nil || !strings.Contains(err.Error(), "retired API pagination.") {
			t.Fatalf("%s: %v", code, err)
		}
	}
	if err := CheckDocument("AGENTS.md", []byte("```go\npagination.Authenticated(page, binding, http.Scopes(scopes))\npagination.AuthorizeActor(page, policy)\n```")); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionRowLockOwners(t *testing.T) {
	for _, pair := range [][2]string{
		{"TransactionScalarOf", "TransactionScalar[S,V]"},
		{"SelectTransactionCorrelatedScalar", "TransactionCorrelatedScalar[O,I,V]"},
		{"SelectTransactionCorrelatedRecord", "TransactionCorrelatedRecord[O,I,R]"},
		{"ProjectTransactionCorrelated", "TransactionCorrelatedRecord[O,I,R]"},
	} {
		for _, method := range []string{"ForUpdate", "ForNoKeyUpdate", "ForShare", "ForKeyShare"} {
			for _, code := range []string{
				fmt.Sprintf("rows.%s.%s()", pair[1], method),
				fmt.Sprintf("rows.%s(selected).%s()", pair[0], method),
				fmt.Sprintf("q := rows.%s(selected)\nq.%s()", pair[0], method),
				fmt.Sprintf("var q rows.%s\nq.%s()", pair[1], method),
			} {
				if err := CheckDocument("AGENTS.md", []byte("```go\n"+code+"\n```")); err != nil {
					t.Fatalf("%s: %v", code, err)
				}
			}
		}
	}
	if err := CheckDocument("AGENTS.md", []byte("```go\norm.Query[User].ForUpdate()\n```")); err == nil || !strings.Contains(err.Error(), "retired API orm.Query.ForUpdate") {
		t.Fatal(err)
	}
}

func TestDocumentOwnerRetirements(t *testing.T) {
	for _, code := range []string{
		"http.Endpoint[P,Q,B,R].WithBodyValidation(rule)",
		"http.AuthenticatedEndpoint[P,Q,B,A,R].WithScopes(scopes)",
		"http.Endpoint.WithPermissions(permission)", "http.Endpoint.WithAuthorization(policy)",
		"http.Endpoint.WithMiddleware(middleware)",
		"modelbinding.Endpoint.WithAuthorization(policy)",
		"e := http.DefineEndpoint(route, query, body, response)\ne.WithBodyValidation(rule)",
		"e := foundryhttp.DefineEndpoint(route, query, body, response)\ne.WithMiddleware(middleware)",
		"e := http.RequireAuthentication(endpoint, authentication, guard)\ne.WithScopes(scope)",
		"validation.Field[Request,string].Rules(rule)", "validation.Rule[string].Check(ctx, input, limits)",
		"f := validation.DefineField[Request](\"name\", selectName)\nf.Rules(rule)",
		"r := validation.Email[string]()\nr.Check(ctx, input, limits)",
		"var r validation.Rule[string]\nr.Check(ctx, input, limits)",
		"t := translations.Text{}\nt.Values().Entries()", "translations.Text.Values().Entries()",
		"orm.Query[User].ForUpdate()", "table := orm.TableOf(b, &user.ID)\nq := table.Query()\nq.ForUpdate()",
		"e := modelbinding.Bind(transport, resolver)\ne.WithAuthorization(policy)",
		"package example\nimport transport \"github.com/weiloon1234/Foundry-Go/http\"\nfunc example() { e := transport.DefineEndpoint(route, query, body, response); e.WithMiddleware(m) }",
	} {
		t.Run(code, func(t *testing.T) {
			for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
				err := check(".claude/skills/api/SKILL.md", []byte("```go\n"+code+"\n```"))
				if err == nil || !strings.Contains(err.Error(), "retired") || strings.Contains(err.Error(), "ambiguous") {
					t.Fatalf("%s: %v", code, err)
				}
			}
		})
	}
}

func TestApplicationDocumentationFrameworkImports(t *testing.T) {
	for _, tc := range []struct {
		code             string
		frameworkRetired bool
	}{
		{"package example\nimport result \"github.com/weiloon1234/Foundry-Go/database/orm/rows\"\nfunc example() { result.Count(items) }", false},
		{"package example\nimport expression \"github.com/weiloon1234/Foundry-Go/database/orm/expr\"\nfunc example() { expression.Select(mapping) }", false},
		{"package example\nimport . \"github.com/weiloon1234/Foundry-Go/database/orm/rows\"\nfunc example() { Count(items) }", true},
	} {
		text := []byte("```go\n" + tc.code + "\n```")
		if err := CheckApplicationDocument("AGENTS.md", text); err == nil || !strings.Contains(err.Error(), "retired API") {
			t.Fatalf("accepted imported retirement: %v", err)
		}
		if err := CheckDocument("README.md", text); (err != nil) != tc.frameworkRetired {
			t.Fatalf("changed framework import detection for %s: %v", tc.code, err)
		}
	}
}

func TestApplicationDocumentationImportNames(t *testing.T) {
	for _, code := range []string{
		"package example\nimport result \"example.test/rows\"\nfunc example() { result.Count(items) }",
		"package example\nimport result \"github.com/weiloon1234/Foundry-Go/database/orm/rows\"\nfunc example(result Custom) { result.Count(items) }",
		"package example\nimport . \"github.com/weiloon1234/Foundry-Go/database/orm/rows\"\nfunc Count(items []Item) {}\nfunc example() { Count(items) }",
	} {
		if err := CheckApplicationDocument("AGENTS.md", []byte("```go\n"+code+"\n```")); err != nil {
			t.Fatalf("application name rejected: %v", err)
		}
	}
}

func TestDocumentRetainedOwners(t *testing.T) {
	for _, code := range []string{
		"Scope.WithMiddleware", "http.Scope.WithMiddleware", "http.AssetMount.WithMiddleware",
		"Report.Check", "ApplicationRule.Check",
		"s := http.DefineScope(\"/api\", \"api\")\ns.WithMiddleware(m)",
		"http.DefineScope(\"/api\", \"api\").WithMiddleware(m).Within(parent).WithMiddleware(n)",
		"rows.ForUpdate(rows.Read(q))", "ForUpdate(rows.Read(q))", "orm.Lock(q)",
		"validation.Rules(field, rule)", "validation.Check(rule, ctx, input, limits)",
		"var text translations.Text\nvalues, loaded := text.Values()\n_ = loaded\nvalues.Entries()",
		"createTokenSession(config)", "authenticatedTransport(config)",
		"client.createTokenSession(config)", "client.authenticatedTransport(config)",
		"hasher, err := password.New(config)\n_ = err\nhasher.Check(ctx, plain, stored)",
		"package example\nimport custom \"example.test/custom\"\nfunc example(r custom.Rule) { r.Check(ctx) }",
		"fmt.Println(\"e.WithScopes(scope)\") // rule.Check is just a comment",
	} {
		if err := CheckDocument("AGENTS.md", []byte("```go\n"+code+"\n```")); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
	}
}

func TestDocumentAmbiguousTeaching(t *testing.T) {
	for _, code := range []string{
		"endpoint.WithBodyValidation(rule)", "endpoint.WithMiddleware(m)", "endpoint.WithScopes(scopes)",
		"endpoint.WithPermissions(permissions)", "endpoint.WithAuthorization(policy)",
		"f.Field.Rules(rule)", "rule.Check(ctx, input, limits)", "text.Values().Entries()", "q.ForUpdate()",
		"Values().Entries()", ".WithScopes(scope)",
		"http := unknown()\nhttp.Scope.WithMiddleware(m)",
		"s := http.DefineScope(\"/api\", \"api\")\ns = unknown()\ns.WithMiddleware(m)",
		"s := http.DefineScope(\"/api\", \"api\")\nif condition { s = unknown() }\ns.WithMiddleware(m)",
		"{ s := http.DefineScope(\"/api\", \"api\") }\ns.WithMiddleware(m)",
	} {
		for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
			err := check("AGENTS.md", []byte("```go\n"+code+"\n```"))
			if err == nil || !strings.Contains(err.Error(), "ambiguous API teaching") || strings.Contains(err.Error(), "retired API") {
				t.Fatalf("%s: %v", code, err)
			}
		}
	}
}

func TestDocumentAmbiguousCallShapes(t *testing.T) {
	for _, tc := range []struct {
		code string
		want string
	}{
		{"productionprofile.Check()", ""},
		{"X.Check()", ""},
		{"rule.Check(ctx)", ""},
		{"rule.Check(ctx, input)", ""},
		{"rule.Check(ctx, input, limits)", "ambiguous API teaching"},
		{"(rule.Check)(ctx, input, limits)", "ambiguous API teaching"},
		{"rule.Check(ctx, input, limits, extra)", ""},
		{"rule.Check(args...)", "ambiguous API teaching"},
		{"rule.Check(ctx, args...)", "ambiguous API teaching"},
		{"rule.Check(makeArgs())", "ambiguous API teaching"},
		{"rule.Check((makeArgs()))", "ambiguous API teaching"},
		{"rule.Check(makeArgs[string]())", "ambiguous API teaching"},
		{"package example\nfunc makeArgs() (int, int, int) { return 1, 2, 3 }\nfunc use(rule interface { Check(int, int, int) }) { rule.Check(makeArgs()) }", "ambiguous API teaching"},
		{"rule.Check(makeArgs(), input)", ""},
		{"validation.Check(rule, ctx, input, limits)", ""},
		{"validation.Check(makeArgs())", ""},
		{"var rule validation.Rule[string]\nrule.Check()", "retired API validation.Rule.Check"},
		{"var rule validation.Rule[string]\nrule.Check(makeArgs())", "retired API validation.Rule.Check"},
		{"var rule validation.Rule[string]\nrule.Check(args...)", "retired API validation.Rule.Check"},
		{"rule.Check", "ambiguous API teaching"},
		{"field.Rules()", "ambiguous API teaching"},
		{"field.Rules(rules...)", "ambiguous API teaching"},
		{"validation.Rules(field, rules...)", ""},
		{"settings.Validate()", ""},
		{"settings.Validate(ctx)", ""},
		{"q.ForUpdate()", "ambiguous API teaching"},
		{"q.ForUpdate(tx)", ""},
		{"q.ForUpdate(args...)", "ambiguous API teaching"},
		{"rows.ForUpdate(rows.Read(q))", ""},
		{"endpoint.WithScopes(scopes)", "ambiguous API teaching"},
		{"endpoint.WithScopes()", ""},
		{"endpoint.WithScopes(first, second)", ""},
		{"endpoint.WithScopes(scopes...)", "ambiguous API teaching"},
		{"endpoint.WithAuthorization(policy)", "ambiguous API teaching"},
		{"endpoint.WithAuthorization()", ""},
		{"endpoint.WithAuthorization(first, second)", ""},
		{"endpoint.WithPermissions()", "ambiguous API teaching"},
		{"endpoint.WithPermissions(permissions...)", "ambiguous API teaching"},
		{"endpoint.WithBodyValidation(rules...)", "ambiguous API teaching"},
		{"endpoint.WithMiddleware(middleware...)", "ambiguous API teaching"},
		{"text.Values().Entries()", "ambiguous API teaching"},
		{"text.Values(input).Entries()", ""},
		{"text.Values().Entries(input)", ""},
		{"text.Values().Entries", "ambiguous API teaching"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := CheckDocument("AGENTS.md", []byte("```go\n"+tc.code+"\n```"))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDocumentRegionsAndHistory(t *testing.T) {
	text := "<!-- app-owned:begin domain -->\nUse `http.Endpoint.WithBodyValidation`.\n<!-- app-owned:end domain -->"
	if err := CheckDocument(".claude/skills/domain/references.md", []byte(text)); err == nil || !strings.Contains(err.Error(), "references.md:2:") {
		t.Fatal(err)
	}
	before := "<!-- orm-example: endpoint-before -->\n````go\nhttp.Endpoint.WithMiddleware(m)\n```\nhttp.Endpoint.WithScopes(scope)\n````\n"
	if err := CheckDocument("docs/guides/orm-migration.md", []byte(before)); err != nil {
		t.Fatal(err)
	}
	if err := CheckDocument("AGENTS.md", []byte(before)); err == nil {
		t.Fatal("app historical label exempted active teaching")
	}
	for _, text := range []string{"Use http.Endpoint.WithAuthorization.", "Use f.Field.Rules.", "Use `translations.Text.Values().Entries()`.", "````go\nhttp.Endpoint.WithScopes(scope)\n````", "```text\nhttp.Endpoint.WithScopes(scope)\n```"} {
		if err := CheckDocument("AGENTS.md", []byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}
