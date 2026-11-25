package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

func TestFindingsNameTheSkillsThatTeachThem(t *testing.T) {
	app := t.TempDir()
	putDocumentation(t, app, "go.mod", "module example.test/app\n")
	putDocumentation(t, app, ".claude/skills/endpoint/SKILL.md", "Foundry guides: `flat-routes#staged-routes`\n")
	putDocumentation(t, app, ".claude/skills/review/SKILL.md", "Foundry guides: `flat-routes#staged-routes`, `model-declarations`\n")
	putDocumentation(t, app, ".claude/skills/review/references/api.md", "Foundry guides: `flat-routes#generated-helpers`\n")
	findings := []diagnostic.Diagnostic{
		diagnostic.RouteNesting.At("routes.go", 42, 9).Say("http.Scope(...)", "route stage built inside another call").To("assign it to a named local first"),
		diagnostic.RouteHelper.At("routes.go", 50, 3).Say("http.JSONBody", "wraps a generated declaration").To("call the helper"),
	}
	var out bytes.Buffer
	if err := printFindings(&out, app, findings); err != nil {
		t.Fatal(err)
	}
	want := "routes.go:42:9: [route-nesting] http.Scope(...): route stage built inside another call; assign it to a named local first. Read docs/guides/flat-routes.md#staged-routes (skill: endpoint, review)\n" +
		"routes.go:50:3: [route-helper] http.JSONBody: wraps a generated declaration; call the helper. Read docs/guides/flat-routes.md#generated-helpers\n"
	if out.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	if err := printFindings(&out, t.TempDir(), findings[:1]); err != nil || strings.Contains(out.String(), "(skill:") || !strings.Contains(out.String(), "Read docs/guides/flat-routes.md#staged-routes") {
		t.Fatalf("a directory without an application module named skills: %q %v", out.String(), err)
	}
	out.Reset()
	if err := printFindings(&out, app, nil); err != nil || out.Len() != 0 {
		t.Fatalf("no findings printed %q %v", out.String(), err)
	}
}

func TestDocsCheckNamesSkillsAndGuides(t *testing.T) {
	app, framework, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, framework, "docs/guides/developer-tooling-and-testing.md", "<a id=\"recipe-reference-guides\"></a>\n# Reference\n")
	putDocumentation(t, app, ".claude/skills/workflow/SKILL.md", "Foundry guides: `developer-tooling-and-testing#recipe-reference-guides`\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: bad\nUse `http.Endpoint.WithScopes`.\n")
	err := checkDocs(t.Context(), app, goBinary, build)
	if err == nil {
		t.Fatal("accepted a malformed declaration and a retired name")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "AGENTS.md:1: [guides-declaration] ") || !strings.HasSuffix(lines[0], "Read docs/guides/developer-tooling-and-testing.md#recipe-reference-guides (skill: workflow)") ||
		!strings.HasPrefix(lines[1], "AGENTS.md:2: [retired-api] http.Endpoint.WithScopes is retired; ") || !strings.HasSuffix(lines[1], "Read docs/guides/developer-tooling-and-testing.md#pinned-documentation-checks") {
		t.Fatalf("findings:\n%s", err)
	}
}
