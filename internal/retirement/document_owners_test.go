package retirement

import (
	"strings"
	"testing"
)

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
		"nullableJoinedText(joined)",
		"package example\nimport transport \"github.com/weiloon1234/Foundry-Go/http\"\nfunc example() { e := transport.DefineEndpoint(route, query, body, response); e.WithMiddleware(m) }",
	} {
		t.Run(code, func(t *testing.T) {
			err := CheckDocument(".claude/skills/api/SKILL.md", []byte("```go\n"+code+"\n```"))
			if err == nil || !strings.Contains(err.Error(), "retired") || strings.Contains(err.Error(), "ambiguous") {
				t.Fatalf("%s: %v", code, err)
			}
		})
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
		err := CheckDocument("AGENTS.md", []byte("```go\n"+code+"\n```"))
		if err == nil || !strings.Contains(err.Error(), "ambiguous API teaching") || strings.Contains(err.Error(), "retired API") {
			t.Fatalf("%s: %v", code, err)
		}
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
	for _, text := range []string{"Use http.Endpoint.WithAuthorization.", "Use f.Field.Rules.", "Use nullableJoinedText for joins.", "Use `translations.Text.Values().Entries()`.", "````go\nhttp.Endpoint.WithScopes(scope)\n````", "```text\nhttp.Endpoint.WithScopes(scope)\n```"} {
		if err := CheckDocument("AGENTS.md", []byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}
