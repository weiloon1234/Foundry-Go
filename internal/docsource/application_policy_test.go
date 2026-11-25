package docsource

import (
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
		{"client/tests/types.ts", skill, "typescript", ""},
		{"internal/policies/admin_policy_test.go", ".claude/skills/testing/SKILL.md", "go", ""},
		{"web/shared/forms/form-feedback.test.ts", ".claude/skills/testing/SKILL.md", "typescript", ""},
		{"internal/policies/admin_policy_test.go", skill, "", "only from the testing skill"},
		{"web/shared/forms/form-feedback.test.ts", skill, "", "only from the testing skill"},
		{"web/shared/forms/form.test.tsx", skill, "", "only from the testing skill"},
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
