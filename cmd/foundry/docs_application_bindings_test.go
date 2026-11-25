package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// listGoFiles stands in for the offline `go list` of the application module: the
// fake go binary of documentationFixture prints it for a listing request.
func listGoFiles(t *testing.T, app string, files ...string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(app)
	if err != nil {
		t.Fatal(err)
	}
	type listed struct {
		Dir                string
		GoFiles, TestFiles []string `json:",omitempty"`
		TestGoFiles        []string `json:",omitempty"`
	}
	byDir := map[string]*listed{}
	var order []string
	for _, file := range files {
		dir := filepath.Dir(file)
		if byDir[dir] == nil {
			byDir[dir] = &listed{Dir: filepath.Join(resolved, dir)}
			order = append(order, dir)
		}
		if strings.HasSuffix(file, "_test.go") {
			byDir[dir].TestGoFiles = append(byDir[dir].TestGoFiles, filepath.Base(file))
		} else {
			byDir[dir].GoFiles = append(byDir[dir].GoFiles, filepath.Base(file))
		}
	}
	var out bytes.Buffer
	for _, dir := range order {
		if err := json.NewEncoder(&out).Encode(byDir[dir]); err != nil {
			t.Fatal(err)
		}
	}
	listing := filepath.Join(t.TempDir(), "list.json")
	if err := os.WriteFile(listing, out.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOUNDRY_TEST_GO_LIST", listing)
}

const (
	exampleBegin = "<!-- app-owned:begin worked-example -->\n"
	exampleEnd   = "<!-- app-owned:end worked-example -->\n"
)

type boundApplication struct {
	app, goBinary string
	build         frameworkinfo.Build
}

// newBoundApplication is an application whose AGENTS.md and skills bind Go and
// TypeScript blocks to its own code, with a worked-example region in each state.
func newBoundApplication(t *testing.T) boundApplication {
	t.Helper()
	app, framework, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, framework, "docs/guides/orm.md", "<a id=\"agent-entry\"></a>\n# Models\n\n<a id=\"recipe-declare-table\"></a>\n## Declare\n")
	putDocumentation(t, app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 1 }\n// doc-example:end page\n")
	putDocumentation(t, app, "internal/pages/page_test.go", "package pages\n\n// doc-example:begin page-test\nfunc TestPage() {}\n// doc-example:end page-test\n")
	putDocumentation(t, app, "web/portals/admin/src/CountriesPage.tsx", "// doc-example:begin countries-page\nexport function CountriesPage() {\n  return <h1>Countries</h1>;\n}\n// doc-example:end countries-page\n")
	putDocumentation(t, app, "web/shared/request-body.ts", "// doc-example:begin request-body\nexport const body = (value: string) => ({ value });\n// doc-example:end request-body\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#agent-entry`\n\nPage model:\n\n<!-- compiled-source: internal/pages/page.go#page -->\n```go\n```\n")
	putDocumentation(t, app, ".claude/skills/web-pages/SKILL.md", "Foundry guides: `orm#agent-entry`\n\n# Pages\n\n"+exampleBegin+
		"<!-- compiled-source: web/portals/admin/src/CountriesPage.tsx#countries-page -->\n```tsx\n```\n\n"+
		"<!-- compiled-source: web/shared/request-body.ts#request-body -->\n```typescript\n```\n"+exampleEnd)
	putDocumentation(t, app, ".claude/skills/foundry-async/SKILL.md", "Foundry guides: `orm#agent-entry`\n\n# Jobs\n\n"+exampleBegin+
		"This application has no worked example for this task yet.\n\nFoundry guides: `orm#recipe-declare-table`\n"+exampleEnd)
	putDocumentation(t, app, ".claude/skills/testing/SKILL.md", "Foundry guides: `orm#agent-entry`\n\n# Tests\n\n"+exampleBegin+
		"<!-- compiled-source: internal/pages/page_test.go#page-test -->\n```go\n```\n"+exampleEnd)
	listGoFiles(t, app, "internal/pages/page.go", "internal/pages/page_test.go")
	var stdout bytes.Buffer
	if err := run(t.Context(), []string{"docs", "render", "--dir", app}, &stdout, &stdout); err != nil {
		t.Fatal(err)
	}
	return boundApplication{app, goBinary, build}
}

func (b boundApplication) check(t *testing.T) error {
	t.Helper()
	return checkDocs(t.Context(), b.app, b.goBinary, b.build)
}

func TestDocsApplicationBindingsAreChecked(t *testing.T) {
	b := newBoundApplication(t)
	if err := b.check(t); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"AGENTS.md":                             "func Page() int { return 1 }",
		".claude/skills/web-pages/SKILL.md":     "export function CountriesPage()",
		".claude/skills/testing/SKILL.md":       "func TestPage() {}",
		".claude/skills/foundry-async/SKILL.md": "This application has no worked example for this task yet.",
	} {
		data, _ := os.ReadFile(filepath.Join(b.app, name))
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s was not rendered: %s", name, data)
		}
	}
}

func TestDocsApplicationBindingRefusals(t *testing.T) {
	fence := func(tag, source string) string {
		return "<!-- compiled-source: " + source + " -->\n```" + tag + "\n```\n"
	}
	tests := []struct {
		name   string
		mutate func(t *testing.T, b boundApplication)
		want   string
	}{
		{"unbound go fence", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n```go\nfunc one() {}\n```\n")
		}, "AGENTS.md:3: [doc-binding] Go block has no compiled source; move the code into a handwritten file under internal/, cmd/, examples/, web/ or client/tests/ and bind it with <!-- compiled-source: path#region -->, then run go tool foundry docs render --dir ."},
		{"unbound typescript fence", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n```typescript\nconst one = 1;\n```\n")
		}, "AGENTS.md:3: [doc-binding] TypeScript block has no compiled source"},
		{"unbound tsx fence", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n```tsx\nconst one = <p />;\n```\n")
		}, "AGENTS.md:3: [doc-binding] TypeScript block has no compiled source"},
		{"unknown tag", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n```rust\nfn main() {}\n```\n")
		}, "AGENTS.md:3: [doc-binding] fenced block has no supported language tag"},
		{"indented code", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\nExample:\n\n    func one() {}\n")
		}, "[doc-binding] indented code is unsupported"},
		{"stale block", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
		}, "AGENTS.md:5: [doc-rendered] compiled source differs from the displayed block; run go tool foundry docs render --dir ."},
		{"region removed from the source", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\nfunc Page() int { return 1 }\n")
		}, "AGENTS.md:6: [doc-region] missing source region page; bind an existing region or add matching begin and end markers to the source, or point the binding at the application's own file and region"},
		{"source deleted", func(t *testing.T, b boundApplication) {
			if err := os.Remove(filepath.Join(b.app, "internal/pages/page.go")); err != nil {
				t.Fatal(err)
			}
		}, "AGENTS.md:6: [doc-marker] compiled source cannot be read; restore the source file, or point the binding at the application's own file and region"},
		{"path outside the policy", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "portals.go", "package main\n\n// doc-example:begin portals\nvar x = 1\n// doc-example:end portals\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "portals.go#portals"))
		}, `AGENTS.md:4: [doc-marker] source "portals.go" is not bindable: Go sources are bindable under internal/, cmd/ and examples/ only`},
		{"typescript outside the roots", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "client/src/index.ts", "// doc-example:begin index\nexport const x = 1;\n// doc-example:end index\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("typescript", "client/src/index.ts#index"))
		}, `source "client/src/index.ts" is not bindable: TypeScript and TSX sources are bindable under web/ and client/tests/ only`},
		{"generated Go file", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "internal/models/page_model_foundry.gen.go", "package models\n\n// doc-example:begin gen\nvar x = 1\n// doc-example:end gen\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/models/page_model_foundry.gen.go#gen"))
		}, `source "internal/models/page_model_foundry.gen.go" is not bindable: generated output is not handwritten`},
		{"generated client file", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "client/generated/contracts.ts", "// doc-example:begin gen\nexport const x = 1;\n// doc-example:end gen\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("typescript", "client/generated/contracts.ts#gen"))
		}, `source "client/generated/contracts.ts" is not bindable: generated output is not handwritten`},
		{"declaration file", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "web/shared/types.d.ts", "// doc-example:begin types\nexport type X = 1;\n// doc-example:end types\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("typescript", "web/shared/types.d.ts#types"))
		}, `source "web/shared/types.d.ts" is not bindable: a declaration file`},
		{"test file from a skill that does not teach tests", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/pages/page_test.go#page-test"))
		}, `AGENTS.md:4: [doc-marker] source "internal/pages/page_test.go" is not bindable: a test file is bindable only from the testing skill`},
		{"TypeScript test file from a skill that does not teach tests", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "web/shared/form.test.ts", "// doc-example:begin form-test\nexport const x = 1;\n// doc-example:end form-test\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("typescript", "web/shared/form.test.ts#form-test"))
		}, `source "web/shared/form.test.ts" is not bindable: a test file is bindable only from the testing skill`},
		{"secret path", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/.env.go#x"))
		}, `source "internal/.env.go" is not bindable: it names a secret input`},
		{"path outside the module", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "../outside.go#x"))
		}, `invalid module-relative source path "../outside.go"`},
		{"nested module", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "internal/sub/go.mod", "module example.test/sub\n")
			putDocumentation(t, b.app, "internal/sub/sub.go", "package sub\n\n// doc-example:begin sub\nvar x = 1\n// doc-example:end sub\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/sub/sub.go#sub"))
		}, `source "internal/sub/sub.go" is not bindable: it belongs to the nested module internal/sub`},
		{"symlinked source", func(t *testing.T, b boundApplication) {
			if err := os.Symlink(filepath.Join(b.app, "internal/pages/page.go"), filepath.Join(b.app, "internal/pages/alias.go")); err != nil {
				t.Fatal(err)
			}
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/pages/alias.go#page"))
		}, "alias.go"},
		{"Go file that no package compiles", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "internal/pages/tagged.go", "//go:build neverselected\n\npackage pages\n\n// doc-example:begin tagged\nvar x = 1\n// doc-example:end tagged\n")
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+fence("go", "internal/pages/tagged.go#tagged"))
		}, "AGENTS.md:3: [doc-inputs] internal/pages/tagged.go#tagged: not a compiler input; bind a file of a package of this module that the default build compiles (no build tag, not generated)"},
		{"marker inside JSX", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "web/portals/admin/src/CountriesPage.tsx", "export function CountriesPage() {\n  return (\n    <div>\n      // doc-example:begin countries-page\n      <h1>Countries</h1>\n      // doc-example:end countries-page\n    </div>\n  );\n}\n")
		}, "web/portals/admin/src/CountriesPage.tsx:4: [doc-region] region marker sits inside JSX"},
		{"empty worked-example", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, ".claude/skills/foundry-async/SKILL.md", "# Jobs\n\n"+exampleBegin+exampleEnd)
		}, ".claude/skills/foundry-async/SKILL.md:3: [doc-worked-example] worked-example region is empty"},
		{"vague none", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, ".claude/skills/foundry-async/SKILL.md", "# Jobs\n\n"+exampleBegin+"None yet, see the guides.\n"+exampleEnd)
		}, ".claude/skills/foundry-async/SKILL.md:3: [doc-worked-example] worked-example region neither binds a block nor makes the explicit statement"},
		{"none naming a missing recipe", func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, ".claude/skills/foundry-async/SKILL.md", "# Jobs\n\n"+exampleBegin+"This application has no worked example for this task yet.\n\nFoundry guides: `orm#recipe-nowhere`\n"+exampleEnd)
		}, `[anchor-missing] anchor "recipe-nowhere" of guide orm: not declared`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := newBoundApplication(t)
			if err := b.check(t); err != nil {
				t.Fatalf("baseline application: %v", err)
			}
			test.mutate(t, b)
			if err := b.check(t); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestDocsApplicationBindingsNeedNoToolchainWithoutGoBindings(t *testing.T) {
	app, _, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, app, "web/shared/request-body.ts", "// doc-example:begin request-body\nexport const body = 1;\n// doc-example:end request-body\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm`\n\n<!-- compiled-source: web/shared/request-body.ts#request-body -->\n```typescript\nexport const body = 1;\n```\n")
	t.Setenv("FOUNDRY_TEST_GO_LIST", filepath.Join(t.TempDir(), "absent"))
	if err := checkDocs(t.Context(), app, goBinary, build); err != nil {
		t.Fatal(err)
	}
}

func TestDocsRenderWritesOnlyBoundBlocks(t *testing.T) {
	b := newBoundApplication(t)
	putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
	prose := "Foundry guides: `orm#agent-entry`\n\nKeep this text and this block:\n\n```sh\nmake verify\n```\n\n"
	putDocumentation(t, b.app, ".claude/skills/other/SKILL.md", prose)
	var stdout bytes.Buffer
	if err := renderDocs(t.Context(), b.app, b.goBinary, true, &stdout); err == nil || !strings.Contains(err.Error(), "AGENTS.md:5: [doc-rendered]") {
		t.Fatalf("render --check: %v", err)
	}
	before := documentationSnapshot(t, b.app)
	delete(before, "AGENTS.md")
	if err := run(t.Context(), []string{"docs", "render", "--dir", b.app}, &stdout, &stdout); err != nil {
		t.Fatal(err)
	}
	after := documentationSnapshot(t, b.app)
	delete(after, "AGENTS.md")
	for name, value := range before {
		if strings.HasSuffix(name, ".md") && !strings.Contains(after[name], strings.SplitN(value, " ", 3)[2]) {
			t.Fatalf("%s changed", name)
		}
	}
	rendered, _ := os.ReadFile(filepath.Join(b.app, "AGENTS.md"))
	if !strings.Contains(string(rendered), "```go\nfunc Page() int { return 2 }\n```") {
		t.Fatalf("AGENTS.md not rendered: %s", rendered)
	}
	if other, _ := os.ReadFile(filepath.Join(b.app, ".claude/skills/other/SKILL.md")); string(other) != prose {
		t.Fatalf("unbound document changed: %q", other)
	}
	if err := renderDocs(t.Context(), b.app, b.goBinary, true, &stdout); err != nil {
		t.Fatal(err)
	}
	if err := b.check(t); err != nil {
		t.Fatal(err)
	}
}

func TestDocsRenderRefusesWithoutWriting(t *testing.T) {
	b := newBoundApplication(t)
	putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
	putDocumentation(t, b.app, ".claude/skills/other/SKILL.md", "<!-- compiled-source: internal/missing.go#x -->\n```go\n```\n")
	before := documentationSnapshot(t, b.app)
	var stdout bytes.Buffer
	if err := run(t.Context(), []string{"docs", "render", "--dir", b.app}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "other/SKILL.md:2: [doc-marker] compiled source cannot be read") {
		t.Fatal(err)
	}
	for name, value := range documentationSnapshot(t, b.app) {
		if before[name] != value {
			t.Fatalf("%s changed although a document was refused", name)
		}
	}
}

func TestDocsRenderIsAllOrNothing(t *testing.T) {
	for _, test := range []struct {
		name, path, text, want string
	}{
		{"unbound fence in another document", ".claude/skills/other/SKILL.md", "# Other\n\n```go\nfunc one() {}\n```\n", "other/SKILL.md:3: [doc-binding] Go block has no compiled source"},
		{"unknown tag", ".claude/skills/other/SKILL.md", "# Other\n\n```rust\nfn main() {}\n```\n", "[doc-binding] fenced block has no supported language tag"},
		{"indented code", ".claude/skills/other/SKILL.md", "# Other\n\n    func one() {}\n", "[doc-binding] indented code is unsupported"},
		{"empty worked-example", ".claude/skills/other/SKILL.md", "# Other\n\n" + exampleBegin + exampleEnd, "[doc-worked-example] worked-example region is empty"},
		{"vague worked-example", ".claude/skills/other/SKILL.md", "# Other\n\n" + exampleBegin + "Soon.\n" + exampleEnd, "[doc-worked-example] worked-example region neither binds a block"},
		{"path outside the policy", ".claude/skills/other/SKILL.md", "<!-- compiled-source: portals.go#x -->\n```go\n```\n", "[doc-marker] source \"portals.go\" is not bindable"},
		{"missing source", ".claude/skills/other/SKILL.md", "<!-- compiled-source: internal/missing.go#x -->\n```go\n```\n", "[doc-marker] compiled source cannot be read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := newBoundApplication(t)
			// A stale block elsewhere would be rendered if the run went ahead.
			putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
			putDocumentation(t, b.app, test.path, test.text)
			before := documentationSnapshot(t, b.app)
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), []string{"docs", "render", "--dir", b.app}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
			if strings.Contains(stdout.String(), "rendered") {
				t.Fatalf("printed success: %q", stdout.String())
			}
			for name, value := range documentationSnapshot(t, b.app) {
				if before[name] != value {
					t.Fatalf("%s changed although a document had a binding finding", name)
				}
			}
		})
	}
}

func TestDocsRenderWritesNothingWhenADocumentCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only file is writable for root")
	}
	b := newBoundApplication(t)
	putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
	putDocumentation(t, b.app, "web/AGENTS.md", "Foundry guides: `orm#agent-entry`\n\n<!-- compiled-source: internal/pages/page.go#page -->\n```go\nold\n```\n")
	nested := filepath.Join(b.app, "web/AGENTS.md")
	if err := os.Chmod(nested, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(nested, 0o644) })
	before := documentationSnapshot(t, b.app)
	var stdout bytes.Buffer
	err := run(t.Context(), []string{"docs", "render", "--dir", b.app}, &stdout, &stdout)
	if err == nil || !strings.Contains(err.Error(), "web/AGENTS.md: [doc-file] document: cannot be opened for writing; make it writable") || strings.Contains(err.Error(), "restore read access") {
		t.Fatalf("%v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("printed %q", stdout.String())
	}
	for name, value := range documentationSnapshot(t, b.app) {
		if before[name] != value {
			t.Fatalf("%s changed although another document could not be written", name)
		}
	}
}

func TestDocsBindingsRefusesWhileADocumentHasABindingFinding(t *testing.T) {
	for name, test := range map[string]struct{ path, text, want string }{
		"unbound fence":        {".claude/skills/other/SKILL.md", "# Other\n\n```typescript\nconst one = 1;\n```\n", "[doc-binding] TypeScript block has no compiled source"},
		"empty worked-example": {".claude/skills/other/SKILL.md", "# Other\n\n" + exampleBegin + exampleEnd, "[doc-worked-example] worked-example region is empty"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBoundApplication(t)
			putDocumentation(t, b.app, test.path, test.text)
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), []string{"docs", "bindings", "--language", "typescript", "--dir", b.app}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) || stdout.Len() != 0 {
				t.Fatalf("stdout %q: %v", stdout.String(), err)
			}
		})
	}
}

func TestDocsCheckNamesTheCauseWhenThePackagesCannotBeListed(t *testing.T) {
	b := newBoundApplication(t)
	t.Setenv("FOUNDRY_TEST_GO_LIST", filepath.Join(t.TempDir(), "absent"))
	err := b.check(t)
	const sentence = "The packages of the application module could not be listed. Check --dir and that `go list ./...` works offline in the module: cat: "
	if err == nil || !strings.Contains(err.Error(), sentence) || !strings.Contains(err.Error(), "No such file or directory") {
		t.Fatalf("%v", err)
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if strings.Contains(line, "could not be listed") && len(line) > 400 {
			t.Fatalf("unbounded line: %q", line)
		}
	}
}

func TestDocsFindingsTeachTheApplicationRecipe(t *testing.T) {
	const application = "Foundry guide: developer-tooling-and-testing#recipe-application-documents"
	b := newBoundApplication(t)
	putDocumentation(t, b.app, "internal/pages/page.go", "package pages\n\n// doc-example:begin page\nfunc Page() int { return 2 }\n// doc-example:end page\n")
	putDocumentation(t, b.app, ".claude/skills/one/SKILL.md", "# One\n\n```go\nfunc one() {}\n```\n")
	putDocumentation(t, b.app, ".claude/skills/two/SKILL.md", "<!-- compiled-source: portals.go#x -->\n```go\n```\n")
	putDocumentation(t, b.app, ".claude/skills/three/SKILL.md", "<!-- compiled-source: internal/pages/page.go#nowhere -->\n```go\n```\n")
	err := b.check(t)
	if err == nil {
		t.Fatal("no findings")
	}
	for _, rule := range []string{"doc-binding", "doc-rendered", "doc-marker", "doc-region"} {
		found := false
		for _, line := range strings.Split(err.Error(), "\n") {
			if strings.Contains(line, "["+rule+"]") {
				found = true
				if !strings.Contains(line, application) {
					t.Errorf("%s does not teach the application recipe: %s", rule, line)
				}
			}
		}
		if !found {
			t.Errorf("no %s finding in %v", rule, err)
		}
	}
}

func TestDocsTextFenceThatLooksLikeCodeNamesBothWaysOut(t *testing.T) {
	b := newBoundApplication(t)
	putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n```text\nfunc one() {}\n```\n")
	err := b.check(t)
	const want = "AGENTS.md:3: [doc-binding] Executable block has no compiled source; move the code into a handwritten file under internal/, cmd/, examples/, web/ or client/tests/ and bind it with <!-- compiled-source: path#region -->, then run go tool foundry docs render --dir . If it is not code, retag it sh or json, or write it as prose. Foundry guide:"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v", err)
	}
}

func TestDocsRefusesSymlinkedAndNoCheckTypeScriptSources(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(t *testing.T, b boundApplication)
		want   string
	}{
		"symlinked directory": {func(t *testing.T, b boundApplication) {
			if err := os.Symlink(filepath.Join(b.app, "web/shared"), filepath.Join(b.app, "web/linked")); err != nil {
				t.Fatal(err)
			}
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n<!-- compiled-source: web/linked/request-body.ts#request-body -->\n```typescript\n```\n")
		}, `source "web/linked/request-body.ts" is not bindable: web/linked is a symbolic link; bind the real file`},
		"dangling symlinked directory": {func(t *testing.T, b boundApplication) {
			if err := os.Symlink(filepath.Join(b.app, "web/gone"), filepath.Join(b.app, "web/linked")); err != nil {
				t.Fatal(err)
			}
			putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n<!-- compiled-source: web/linked/request-body.ts#request-body -->\n```typescript\n```\n")
		}, `web/linked is a symbolic link; bind the real file`},
		"ts-nocheck": {func(t *testing.T, b boundApplication) {
			putDocumentation(t, b.app, "web/shared/request-body.ts", "// @ts-nocheck\n// doc-example:begin request-body\nexport const body = 1;\n// doc-example:end request-body\n")
		}, `source "web/shared/request-body.ts" is not bindable: it contains @ts-nocheck`},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBoundApplication(t)
			test.mutate(t, b)
			if err := b.check(t); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestDocsBindingsListsSourcesOnePathPerLine(t *testing.T) {
	b := newBoundApplication(t)
	for language, want := range map[string]string{
		"go":         "internal/pages/page.go\ninternal/pages/page_test.go\n",
		"typescript": "web/portals/admin/src/CountriesPage.tsx\nweb/shared/request-body.ts\n",
	} {
		var stdout, stderr bytes.Buffer
		if err := run(t.Context(), []string{"docs", "bindings", "--language", language, "--dir", b.app}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != want || stderr.Len() != 0 {
			t.Fatalf("%s: stdout %q stderr %q; want %q", language, stdout.String(), stderr.String(), want)
		}
	}
	// A document that cannot bind is an error and prints nothing.
	putDocumentation(t, b.app, "AGENTS.md", "Foundry guides: `orm`\n\n"+"<!-- compiled-source: portals.go#x -->\n```go\n```\n")
	var stdout bytes.Buffer
	if err := run(t.Context(), []string{"docs", "bindings", "--language", "go", "--dir", b.app}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "is not bindable") || stdout.Len() != 0 {
		t.Fatalf("%q %v", stdout.String(), err)
	}
	empty, _, _, _ := documentationFixture(t, "match")
	stdout.Reset()
	if err := run(t.Context(), []string{"docs", "bindings", "--language", "typescript", "--dir", empty}, &stdout, &stdout); err != nil || stdout.Len() != 0 {
		t.Fatalf("%q %v", stdout.String(), err)
	}
}

func TestDocsBindingsAndRenderUsage(t *testing.T) {
	for _, args := range [][]string{
		{"docs", "bindings"}, {"docs", "bindings", "--language", "python"}, {"docs", "bindings", "--language", "go", "extra"},
		{"docs", "render", "extra"}, {"docs", "render", "--write"},
	} {
		if err := run(t.Context(), args, bytes.NewBuffer(nil), bytes.NewBuffer(nil)); cli.Status(err) != cli.InvalidUsage {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
