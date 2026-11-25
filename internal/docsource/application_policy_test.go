package docsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeApplicationFile(t *testing.T, root, name, text string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationPolicyApprovesOnlyHandwrittenApplicationCode(t *testing.T) {
	root := t.TempDir()
	writeApplicationFile(t, root, "examples/jobs/go.mod", "module example.test/jobs\n")
	policy := ApplicationPolicy()
	const skill = ".claude/skills/model/SKILL.md"
	for _, test := range []struct {
		name, document, language, want string
	}{
		{"internal/pages/page_model.go", skill, "go", ""},
		{"cmd/artisan/main.go", skill, "go", ""},
		{"examples/projects/project_model.go", skill, "go", ""},
		{"web/portals/admin/src/CountriesPage.tsx", skill, "tsx", ""},
		{"web/shared/forms/request-body.ts", skill, "typescript", ""},
		{"client/tests/types.ts", ".claude/skills/testing/SKILL.md", "typescript", ""},
		{"web/tests/browser/admin-editor.spec.ts", ".claude/skills/testing/SKILL.md", "typescript", ""},
		{"web/portals/admin/src/Page.spec.tsx", ".claude/skills/testing/SKILL.md", "tsx", ""},
		{"internal/policies/admin_policy_test.go", ".claude/skills/testing/SKILL.md", "go", ""},
		{"web/shared/forms/form-feedback.test.ts", ".claude/skills/testing/SKILL.md", "typescript", ""},
		{"internal/policies/admin_policy_test.go", skill, "", "only from the testing skill"},
		{"web/shared/forms/form-feedback.test.ts", skill, "", "only from the testing skill"},
		{"web/shared/forms/form.test.tsx", skill, "", "only from the testing skill"},
		{"client/tests/types.ts", skill, "", "only from the testing skill"},
		{"web/tests/browser/admin-editor.spec.ts", skill, "", "only from the testing skill"},
		{"web/shared/forms/form.spec.ts", skill, "", "only from the testing skill"},
		{"web/portals/admin/src/Page.spec.tsx", skill, "", "only from the testing skill"},
		{"web/tests/helpers.ts", skill, "", "only from the testing skill"},
		{"web/shared/forms/Form.TEST.ts", skill, "", "only from the testing skill"},
		{"internal/policies/Admin_Test.go", skill, "", "only from the testing skill"},
		{"web/DIST/o.ts", skill, "", "dist"},
		{"web/Node_Modules/lib/index.ts", skill, "", "files under node_modules"},
		{"internal/Testdata/page.go", skill, "", "testdata"},
		{"internal/Deploy/steps.go", skill, "", "deploy"},
		{"internal/Config/settings.go", skill, "", "config"},
		{"internal/.ENV.go", skill, "", "secret input"},
		{"internal/.Env/x.go", skill, "", "secret input"},
		{"client/Generated/contracts.ts", skill, "", "generated output"},
		{"internal/models/page_model_FOUNDRY.GEN.go", skill, "", "generated output"},
		{"web/shared/Types.D.TS", skill, "", "declaration file"},
		{"portals.go", skill, "", "Go sources are bindable under internal/, cmd/ and examples/ only"},
		{"web/portals/main.go", skill, "", "Go sources are bindable under internal/, cmd/ and examples/ only"},
		{"internal/pages/page.ts", skill, "", "TypeScript and TSX sources are bindable under web/ and client/tests/ only"},
		{"client/src/index.ts", skill, "", "TypeScript and TSX sources are bindable under web/ and client/tests/ only"},
		{"internal/models/page_model_foundry.gen.go", skill, "", "generated output"},
		{"client/generated/contracts.ts", skill, "", "generated output"},
		{"web/shared/types.d.ts", skill, "", "declaration file"},
		{"web/node_modules/lib/index.ts", skill, "", "node_modules"},
		{"web/portals/dist/app.ts", skill, "", "dist"},
		{"internal/pages/testdata/page.go", skill, "", "testdata"},
		{"internal/deploy/steps.go", skill, "", "deploy"},
		{"internal/config/settings.go", skill, "", "config"},
		{"internal/.env.go", skill, "", "secret input"},
		{"examples/jobs/job.go", skill, "", "nested module examples/jobs"},
		{"../outside.go", skill, "", "invalid module-relative source path"},
		{"/etc/passwd.go", skill, "", "invalid module-relative source path"},
		{"internal//page.go", skill, "", "invalid module-relative source path"},
		{"internal/page.go", skill + "x", "go", ""},
		{"internal/page.txt", skill, "", "bindable files are"},
		{"config/app.toml", skill, "", "bindable files are"},
	} {
		language, err := policy.approve(root, test.document, test.name)
		if test.want == "" && (err != nil || language != test.language) {
			t.Errorf("%s from %s: %q, %v; want %q", test.name, test.document, language, err, test.language)
		}
		if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%s from %s: %v; want %q", test.name, test.document, err, test.want)
		}
	}
}

func TestFrameworkPolicyIsUnchanged(t *testing.T) {
	policy := FrameworkPolicy()
	for name, language := range map[string]string{
		"tests/fixtures/consumer/recipe/example.go":                                "go",
		"tests/fixtures/goway/recipe/example.go":                                   "go",
		"tests/fixtures/consumer/clientcontracts/testdata/example.ts":              "typescript",
		"tests/fixtures/consumer/clientcontracts/testdata/limits/example.ts":       "typescript",
		"tests/fixtures/consumer/clientcontracts/testdata/table-bindings/table.ts": "typescript",
	} {
		if got, err := policy.approve("", "docs/guides/x.md", name); err != nil || got != language {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"internal/example.go", "web/app.tsx", "tests/fixtures/consumer/page.tsx", "tests/fixtures/consumer/x_foundry.gen.go", "tests/fixtures/consumer/clientcontracts/testdata/types.d.ts"} {
		if _, err := policy.approve("", "docs/guides/x.md", name); err == nil || !strings.Contains(err.Error(), "outside approved handwritten fixture inputs") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTSXRegionsWrapWholeDeclarations(t *testing.T) {
	const wrapped = `// doc-example:begin page
export function Page() {
  const items = useItems();
  return (
    <ul>
      {items.map((item) => <li key={item}>{item}</li>)}
    </ul>
  );
}
// doc-example:end page

export const generic = <T,>(value: T) => value;
`
	parsed, err := parseSource(wrapped, "tsx")
	if err != nil {
		t.Fatal(err)
	}
	if region := parsed.regions["page"]; !strings.HasPrefix(region.text, "export function Page()") || !strings.HasSuffix(region.text, "}\n") {
		t.Fatalf("region = %q", region.text)
	}
	const statements = "export function Page() {\n  // doc-example:begin state\n  const items = useItems();\n  // doc-example:end state\n  return <ul>{items}</ul>;\n}\n"
	if _, err := parseSource(statements, "tsx"); err != nil {
		t.Fatalf("statements before the return: %v", err)
	}
	for name, test := range map[string]struct{ source, want string }{
		"JSX text":          {"export const page = (\n  <div>\n    // doc-example:begin inside\n    <span />\n    // doc-example:end inside\n  </div>\n);\n", "line 3: region marker sits inside JSX"},
		"JSX expression":    {"export const page = (\n  <div>\n    {\n      // doc-example:begin inside\n      value\n    }\n  </div>\n);\n", "line 4: region marker sits inside JSX"},
		"JSX attribute":     {"export const page = <Select\n  // doc-example:begin inside\n  value={1} />;\n", "line 2: region marker sits inside JSX"},
		"fragment":          {"export const page = <>\n  // doc-example:begin inside\n</>;\n", "line 2: region marker sits inside JSX"},
		"generic component": {"export const page = <Select<string> value=\"a\">\n  // doc-example:begin inside\n</Select>;\n", "line 2: region marker sits inside JSX"},
		"template literal":  {"export const text = `\n// doc-example:begin inside\n`;\n", "line 2: region marker must be an actual line comment"},
	} {
		if _, err := parseSource(test.source, "tsx"); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v; want %q", name, err, test.want)
		}
	}
	// A comparison and a generic call are not elements, so a marker after them is a comment.
	const operators = "const a = b < c;\nconst d = f<string>(x);\n// doc-example:begin ok\nconst e = 1;\n// doc-example:end ok\n"
	if _, err := parseSource(operators, "tsx"); err != nil {
		t.Fatal(err)
	}
	// Plain TypeScript never has JSX: a type assertion-like text is not an element.
	if _, err := parseSource("const a = <T,>(x: T) => x;\n// doc-example:begin ok\nconst e = 1;\n// doc-example:end ok\n", "typescript"); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationPolicyRefusesSymlinksAndNoCheck(t *testing.T) {
	root := t.TempDir()
	writeApplicationFile(t, root, "internal/pages/page.go", "package pages\n")
	writeApplicationFile(t, root, "web/src/real/z.ts", "export const z = 1;\n")
	writeApplicationFile(t, root, "web/src/loose.ts", "// @ts-nocheck\nexport const loose: number = 'x';\n")
	writeApplicationFile(t, root, "web/src/loose-block.tsx", "/* @ts-nocheck */\nexport const view = <p />;\n")
	if err := os.Symlink(filepath.Join(root, "internal/pages/page.go"), filepath.Join(root, "internal/pages/alias.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "web/src/real"), filepath.Join(root, "web/src/lnk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "web/src/gone"), filepath.Join(root, "web/src/dangling")); err != nil {
		t.Fatal(err)
	}
	policy := ApplicationPolicy()
	const skill = ".claude/skills/model/SKILL.md"
	for name, want := range map[string]string{
		"internal/pages/alias.go":  `internal/pages/alias.go is a symbolic link; bind the real file`,
		"web/src/lnk/z.ts":         `web/src/lnk is a symbolic link; bind the real file`,
		"web/src/dangling/z.ts":    `web/src/dangling is a symbolic link; bind the real file`,
		"internal/pages/page.go":   "",
		"web/src/real/z.ts":        "",
		"web/src/never/missing.ts": "",
	} {
		_, err := policy.approve(root, skill, name)
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: %v; want %q", name, err, want)
		}
	}
	for _, name := range []string{"web/src/loose.ts", "web/src/loose-block.tsx"} {
		language, err := policy.approve(root, skill, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadSource(policy, root, name, language); err == nil || !strings.Contains(err.Error(), "contains @ts-nocheck, which would hide the file from the application's type-check") {
			t.Errorf("%s: %v", name, err)
		}
		// The framework policy is unchanged: it does not read application TypeScript.
		if _, err := loadSource(FrameworkPolicy(), root, name, language); err != nil {
			t.Errorf("%s under the framework policy: %v", name, err)
		}
	}
	if _, err := loadSource(policy, root, "web/src/real/z.ts", "typescript"); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationFindingsTeachTheApplicationRecipe(t *testing.T) {
	const (
		application = "Foundry guide: developer-tooling-and-testing#recipe-application-documents"
		author      = "Foundry guide: developer-tooling-and-testing#recipe-author-examples"
	)
	root := t.TempDir()
	writeApplicationFile(t, root, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nvar x = 1\n// doc-example:end page\n")
	for name, text := range map[string]string{
		"doc-binding":        "```go\nvar x = 1\n```\n",
		"doc-marker":         "<!-- compiled-source: portals.go#x -->\n```go\n```\n",
		"doc-region":         "<!-- compiled-source: internal/pages/page.go#nowhere -->\n```go\n```\n",
		"doc-rendered":       "<!-- compiled-source: internal/pages/page.go#page -->\n```go\nold\n```\n",
		"doc-agent-entry":    "## Agent entry\n\nExplain the tasks here instead of linking them.\n",
		"doc-section-anchor": "<a id=\"recipe-first\"></a>\nProse where the heading belongs.\n",
	} {
		writeApplicationFile(t, root, ".claude/skills/a/SKILL.md", text)
		documents, err := LoadDocuments(ApplicationPolicy(), root, []string{".claude/skills/a/SKILL.md"}, nil)
		failure := errors.Join(err, CheckBindings(documents), Check(documents)).Error()
		if !strings.Contains(failure, "["+name+"]") || !strings.Contains(failure, application) || strings.Contains(failure, author) {
			t.Errorf("%s in an application: %s", name, failure)
		}
	}
	// The framework policy keeps the framework-author recipe.
	for name, text := range map[string]string{
		"doc-binding":        "```go\nvar x = 1\n```\n",
		"doc-marker":         "<!-- compiled-source: internal/x.go#x -->\n```go\n```\n",
		"doc-agent-entry":    "## Agent entry\n\nExplain the tasks here instead of linking them.\n",
		"doc-section-anchor": "<a id=\"recipe-first\"></a>\nProse where the heading belongs.\n",
	} {
		writeApplicationFile(t, root, "docs/guides/a.md", text)
		documents, err := LoadDocuments(FrameworkPolicy(), root, []string{"docs/guides/a.md"}, nil)
		failure := errors.Join(err, CheckBindings(documents)).Error()
		if !strings.Contains(failure, "["+name+"]") || !strings.Contains(failure, author) || strings.Contains(failure, application) {
			t.Errorf("%s in the framework: %s", name, failure)
		}
	}
}
