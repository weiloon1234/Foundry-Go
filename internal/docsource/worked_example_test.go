package docsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	exampleBegin = "<!-- app-owned:begin worked-example -->\n"
	exampleEnd   = "<!-- app-owned:end worked-example -->\n"
	exampleNone  = "This application has no worked example for this task yet.\n\nFoundry guides: `orm#recipe-declare-table`\n"
)

func workedExampleFindings(t *testing.T, text string) string {
	t.Helper()
	src := source{language: "go", regions: map[string]excerpt{"example": {text: "x := 1\n", first: 1, last: 1}}}
	document, err := parseDocument(ApplicationPolicy(), nil, ".claude/skills/model/SKILL.md", []byte(text), func(string) (source, error) { return src, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings([]Document{document}); err != nil {
		return err.Error()
	}
	return ""
}

func TestWorkedExampleRegionStates(t *testing.T) {
	const bound = "<!-- compiled-source: internal/a/a.go#example -->\n```go\nx := 1\n```\n"
	for _, test := range []struct{ name, text, want string }{
		{"bound block directly after the marker", exampleBegin + bound + exampleEnd, ""},
		{"bound block with a lead sentence", exampleBegin + "How the pages model does it.\n\n" + bound + "\n" + exampleEnd, ""},
		{"explicit none", exampleBegin + exampleNone + exampleEnd, ""},
		{"explicit none, adjacent lines", exampleBegin + strings.Replace(exampleNone, "\n\n", "\n", 1) + exampleEnd, ""},
		{"no region at all", "# Skill\n", ""},
		{"empty", exampleBegin + exampleEnd, "worked-example region is empty"},
		{"blank only", exampleBegin + "\n\n" + exampleEnd, "worked-example region is empty"},
		{"vague none", exampleBegin + "None yet.\n" + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"statement without a guide", exampleBegin + "This application has no worked example for this task yet.\n" + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"guide without the statement", exampleBegin + "Foundry guides: `orm#recipe-declare-table`\n" + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"guide is not a recipe anchor", exampleBegin + strings.Replace(exampleNone, "recipe-declare-table", "agent-entry", 1) + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"guide without an anchor", exampleBegin + strings.Replace(exampleNone, "#recipe-declare-table", "", 1) + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"two guides", exampleBegin + exampleNone + "Foundry guides: `orm#recipe-other`\n" + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"extra prose with the statement", exampleBegin + exampleNone + "\nSee later.\n" + exampleEnd, "neither binds a block nor makes the explicit statement"},
		{"statement beside a bound block", exampleBegin + exampleNone + "\n" + bound + exampleEnd, "holds a bound block and also says there is no worked example"},
		{"unbound fence only", exampleBegin + "```go\nx := 1\n```\n" + exampleEnd, "Go block has no compiled source"},
		{"duplicate region", exampleBegin + bound + exampleEnd + exampleBegin + bound + exampleEnd, "duplicate worked-example region"},
		{"end without begin", exampleEnd, "ends without a matching begin marker"},
		{"never closed", exampleBegin + bound, "never closed"},
		{"other regions are not checked", "<!-- app-owned:begin this-app -->\n<!-- app-owned:end this-app -->\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := workedExampleFindings(t, test.text)
			if test.want == "" && got != "" || test.want != "" && !strings.Contains(got, test.want) {
				t.Fatalf("findings %q; want %q", got, test.want)
			}
			if test.want != "" && test.want != "Go block has no compiled source" && !strings.Contains(got, "[doc-worked-example]") && !strings.Contains(got, "[doc-binding]") {
				t.Fatalf("no rule in %q", got)
			}
		})
	}
}

func TestWorkedExampleRegionIsOnlyCheckedForApplications(t *testing.T) {
	document, err := parseDocument(FrameworkPolicy(), nil, "docs/guides/x.md", []byte(exampleBegin+exampleEnd), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings([]Document{document}); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationDocumentsBindAndRender(t *testing.T) {
	root := t.TempDir()
	writeApplicationFile(t, root, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 1 }\n// doc-example:end page\n")
	writeApplicationFile(t, root, "web/page.tsx", "// doc-example:begin view\nexport function View() { return <p>one</p>; }\n// doc-example:end view\n")
	writeApplicationFile(t, root, "internal/pages/page_test.go", "package pages\n\nfunc TestPage() {}\n")
	skill := func(name, body string) string {
		writeApplicationFile(t, root, ".claude/skills/"+name+"/SKILL.md", body)
		return ".claude/skills/" + name + "/SKILL.md"
	}
	stale := "# Pages\n\nProse stays.\n\n" + exampleBegin +
		"<!-- compiled-source: internal/pages/page.go#page -->\n```go\nold\n```\n\n" +
		"<!-- compiled-source: web/page.tsx#view -->\n```tsx\nold\n```\n" + exampleEnd + "\n```sh\nmake x\n```\n"
	model := skill("model", stale)
	testing := skill("testing", "<!-- compiled-source: internal/pages/page_test.go -->\n```go\nold\n```\n")
	other := skill("other", "<!-- compiled-source: internal/pages/page_test.go -->\n```go\nold\n```\n")
	names := []string{model, testing, other}

	documents, err := LoadDocuments(ApplicationPolicy(), root, names, nil)
	if err == nil || !strings.Contains(err.Error(), other+":2: [doc-marker]") || !strings.Contains(err.Error(), "only from the testing skill") {
		t.Fatalf("a test file bound by another skill: %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("documents = %d", len(documents))
	}
	if err := Check(documents); err == nil || !strings.Contains(err.Error(), "run go tool foundry docs render --dir .") {
		t.Fatalf("stale blocks: %v", err)
	}
	before := map[string][]byte{}
	for _, name := range []string{"internal/pages/page.go", "web/page.tsx", other} {
		before[name], _ = os.ReadFile(filepath.Join(root, name))
	}
	if err := Render(root, documents); err != nil {
		t.Fatal(err)
	}
	for name, want := range before {
		if got, _ := os.ReadFile(filepath.Join(root, name)); string(got) != string(want) {
			t.Fatalf("%s changed", name)
		}
	}
	got, _ := os.ReadFile(filepath.Join(root, model))
	const want = "# Pages\n\nProse stays.\n\n" + exampleBegin +
		"<!-- compiled-source: internal/pages/page.go#page -->\n```go\nfunc Page() int { return 1 }\n```\n\n" +
		"<!-- compiled-source: web/page.tsx#view -->\n```tsx\nexport function View() { return <p>one</p>; }\n```\n" + exampleEnd + "\n```sh\nmake x\n```\n"
	if string(got) != want {
		t.Fatalf("rendered %q", got)
	}
	documents, err = LoadDocuments(ApplicationPolicy(), root, []string{model, testing}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(CheckBindings(documents), Check(documents)); err != nil {
		t.Fatal(err)
	}
	if got := Bindings(documents, "go"); strings.Join(got, ",") != "internal/pages/page.go,internal/pages/page_test.go" {
		t.Fatalf("go bindings %v", got)
	}
	if got := Bindings(documents, "typescript"); strings.Join(got, ",") != "web/page.tsx" {
		t.Fatalf("typescript bindings %v", got)
	}
}
