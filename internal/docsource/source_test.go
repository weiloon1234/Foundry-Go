package docsource

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

func TestAgentNavigationLines(t *testing.T) {
	for _, heading := range []string{"## Agent entry", "<a id=\"agent-entry\"></a>\n## Tasks"} {
		text := heading + "\n\n- Read: [Read](#recipe-read)\n\n## Background\n\n[Other](#other)\n"
		document, err := parseDocument(nil, "guide.md", []byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Count(heading, "\n") + 3
		if len(document.AgentLines) != 1 || document.AgentLines[0] != want {
			t.Fatalf("navigation lines = %v; want [%d]", document.AgentLines, want)
		}
	}
}

func TestRecipeAnchorRequiresHeading(t *testing.T) {
	for _, tail := range []string{"<!-- explanation -->\n## Read", "Some prose\n## Read", "<a id=\"recipe-other\"></a>\n## Read", "<a id=\"other\"></a>\n## Read", "Read\n----", ""} {
		_, err := parseDocument(nil, "guide.md", []byte("<a id=\"recipe-read\"></a>\n"+tail), nil)
		if err == nil || !strings.Contains(err.Error(), "must be followed by a # heading") {
			t.Fatalf("displaced anchor %q: %v", tail, err)
		}
	}
	if _, err := parseDocument(nil, "guide.md", []byte("<a id=\"recipe-read\"></a>\n\n## Read\n"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestLiteralMarkersCannotClaimCompilerProof(t *testing.T) {
	const region = "// doc-example:begin hidden\nthis is invalid executable source\n// doc-example:end hidden\n"
	for _, test := range []struct{ name, file, source string }{
		{"Go raw string", "tests/fixtures/consumer/example/example.go", "package example\nconst text = `\n" + region + "`\n"},
		{"Go block comment", "tests/fixtures/consumer/example/example.go", "package example\n/*\n" + region + "*/\n"},
		{"TS template", "tests/fixtures/consumer/clientcontracts/testdata/example.ts", "const text = `\n" + region + "`;\n"},
		{"TS block comment", "tests/fixtures/consumer/clientcontracts/testdata/example.ts", "/*\n" + region + "*/\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			put := func(name, text string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			put(test.file, test.source)
			put("docs/guides/example.md", "<!-- compiled-source: "+test.file+"#hidden -->\n```go\nthis is invalid executable source\n```\n")
			docs := []Document{{Inclusions: []Inclusion{{Source: test.file, Region: "hidden"}}}}
			if strings.HasSuffix(test.file, ".go") {
				input, _ := json.Marshal(map[string]any{"Dir": filepath.Join(root, filepath.Dir(test.file)), "GoFiles": []string{"example.go"}})
				if err := CheckGoInputs(root, docs, strings.NewReader(string(input))); err != nil {
					t.Fatal(err)
				}
			} else {
				put(TypeScriptInputs, `["example.ts"]`)
				if err := CheckTypeScriptInputs(root, docs); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(root, nil); err == nil || !strings.Contains(err.Error(), "actual line comment") {
				t.Fatalf("literal marker accepted as compiler proof: %v", err)
			}
		})
	}
}

func TestSourceCommentContexts(t *testing.T) {
	for _, language := range []string{"go", "typescript"} {
		text := "const value = \"/* not a comment */\"\n// doc-example:begin value\nvar result = value\n// doc-example:end value\n"
		if _, err := parseSource(text, language); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{"const text = `escaped \\` still text\n", "const text = `outer ${`nested\n"} {
		if _, err := parseSource(prefix+"// doc-example:begin hidden\ninvalid code\n// doc-example:end hidden\n`}`;", "typescript"); err == nil {
			t.Fatal("template marker accepted")
		}
	}
	if _, err := parseSource("const pattern = /[\"'`]/;\n// doc-example:begin value\nconst value = 1;\n// doc-example:end value\n", "typescript"); err != nil {
		t.Fatal(err)
	}
}

func TestRegions(t *testing.T) {
	tests := []struct {
		name, text, want string
		bad              bool
	}{
		{"exact", "outside\n// doc-example:begin example\n\talpha\n\t\tbeta\n\t\n// doc-example:end example\nafter\n", "alpha\n\tbeta\n", false},
		{"boundary padding", "// doc-example:begin example\n\n \t\n\talpha\n\t\n\t\tbeta  \n\t\n\n// doc-example:end example\n", "alpha\n\n\tbeta  \n", false},
		{"blank only", "// doc-example:begin example\n\n \t\n// doc-example:end example\n", "", false},
		{"mixed indent", "// doc-example:begin example\n \talpha\n  beta\n// doc-example:end example\n", "\talpha\n beta\n", false},
		{"empty", "// doc-example:begin example\n// doc-example:end example\n", "", false},
		{"duplicate", "// doc-example:begin example\n// doc-example:end example\n// doc-example:begin example\n// doc-example:end example\n", "", true},
		{"overlap", "// doc-example:begin example\n// doc-example:begin other\n", "", true},
		{"missing begin", "// doc-example:end example\n", "", true},
		{"mismatch", "// doc-example:begin example\n// doc-example:end other\n", "", true},
		{"unclosed", "// doc-example:begin example\nalpha\n", "", true},
		{"malformed", "// doc-example:begin bad name\n", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := parseSource(test.text, "go")
			if test.bad {
				if err == nil {
					t.Fatal("accepted invalid region")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := source.regions["example"].text; got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestWholeFileRetainsBoundaryBlankLines(t *testing.T) {
	text := "\n\npackage example\n\n"
	source, err := parseSource(text, "go")
	if err != nil || source.whole.text != text {
		t.Fatalf("whole-file whitespace changed: %q, %v", source.whole.text, err)
	}
}

func TestObsoleteSnippetConventionsRejected(t *testing.T) {
	for _, marker := range []string{"<!-- go-source: example.go -->", "<!-- compiled-examples -->", "<!-- orm-example: example -->", "<!-- orm-example: example-before -->"} {
		if _, err := parseDocument(nil, "guide.md", []byte(marker+"\n"), nil); err == nil || !strings.Contains(err.Error(), "obsolete guide snippet marker") {
			t.Fatalf("accepted obsolete marker %q: %v", marker, err)
		}
		if _, err := parseDocument(nil, "guide.md", []byte("```markdown\n"+marker+"\n```\n"), nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderPreservesProseAndIsIdempotent(t *testing.T) {
	original := "# Guide\r\n\r\nAuthored prose.\r\n<!-- compiled-source: example.ts#example -->\r\n\r\n~~~typescript\nstale\n~~~\n\nAfter.\n"
	src, err := parseSource("const before = 0;\n// doc-example:begin example\n  const fence = \" ``` \";\n  const value = 1;\n// doc-example:end example\n", "typescript")
	if err != nil {
		t.Fatal(err)
	}
	read := func(string) (source, error) { return src, nil }
	first, err := parseDocument(nil, "guide.md", []byte(original), read)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "stale", "const fence = \" ``` \";\nconst value = 1;", 1)
	if string(first.Rendered) != want {
		t.Fatalf("got %q, want %q", first.Rendered, want)
	}
	if err := Check([]Document{first}); err == nil {
		t.Fatal("edited fence passed equality")
	}
	second, err := parseDocument(nil, "guide.md", first.Rendered, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check([]Document{second}); err != nil {
		t.Fatal(err)
	}
	src.regions["example"] = excerpt{text: "changed\n"}
	changed, err := parseDocument(nil, "guide.md", first.Rendered, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check([]Document{changed}); err == nil {
		t.Fatal("changed source passed equality")
	}
}

func TestDocumentBoundaries(t *testing.T) {
	src, _ := parseSource("// doc-example:begin example\nx\n// doc-example:end example\n", "go")
	src.language = "go"
	read := func(string) (source, error) { return src, nil }
	for _, test := range []struct {
		name, text string
		bad        bool
	}{
		{"whole file", "<!-- compiled-source: example.go -->\n```go\nold\n```", false},
		{"missing region", "<!-- compiled-source: example.go#missing -->\n```go\n```", true},
		{"intervening prose", "<!-- compiled-source: example.go#example -->\nprose\n```go\n```", true},
		{"missing fence", "<!-- compiled-source: example.go#example -->\n", true},
		{"malformed directive", "<!-- compiled-source: example.go# -->\n```go\n```", true},
		{"missing closing fence", "<!-- compiled-source: example.go -->\n```go\nx", true},
		{"recipe child", "<a id=\"recipe-read\"></a>\n## Read\n### Child\n```go\nx\n```", true},
		{"recipe sibling", "<a id=\"recipe-read\"></a>\n## Read\n## Reference\n```go\nx\n```", true},
		{"quoted recipe", "<a id=\"recipe-read\"></a>\n## Read\n> ```go\nx\n> ```", true},
		{"listed recipe", "<a id=\"recipe-read\"></a>\n## Read\n- ```typescript\nx\n  ```", true},
		{"typescript alias", "<a id=\"recipe-read\"></a>\n## Read\n```ts\nx\n```", true},
		{"nonexecutable", "<a id=\"recipe-read\"></a>\n## Read\n```json\n{}\n```", false},
		{"agent link", "<a id=\"agent-entry\"></a>\n## Agent entry\n- Read: [Read](#recipe-read)\n", false},
		{"agent prose", "## Agent entry\nUse this API.\n", true},
		{"agent fence", "## Agent entry\n```sh\ncommand\n```", true},
		{"syntax in fence", "```markdown\n<!-- compiled-source: absent.go -->\n<a id=\"recipe-read\"></a>\n```\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parseDocument(nil, "guide.md", []byte(test.text), read)
			if err == nil {
				err = CheckBindings([]Document{doc})
			}
			if (err != nil) != test.bad {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRepeatedInclusionAcrossDocuments(t *testing.T) {
	root := t.TempDir()
	file := "tests/fixtures/consumer/recipe/example.go"
	writeInput(t, root, file, "package recipe\n// doc-example:begin value\nvar Value = 1\n// doc-example:end value\n")
	for _, name := range []string{"docs/guides/one.md", "docs/guides/two.md", "AGENTS.md", ".claude/skills/example/SKILL.md"} {
		writeInput(t, root, name, "<!-- compiled-source: "+file+"#value -->\n```go\nvar Value = 1\n```\n")
	}
	docs, err := Load(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 4 {
		t.Fatalf("loaded %d documents", len(docs))
	}
	if err := Check(docs); err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings(docs); err != nil {
		t.Fatal(err)
	}
}

func writeInput(t *testing.T, root, name, data string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSourcePaths(t *testing.T) {
	root := t.TempDir()
	good := "tests/fixtures/consumer/recipe/example.go"
	writeInput(t, root, good, "package recipe\n")
	if _, err := readSource(root, good); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"/tmp/example.go", "../example.go", "tests/fixtures/consumer/../example.go", "internal/example.go", "tests/fixtures/consumer/recipe/example.txt", "tests/fixtures/consumer/clientcontracts/testdata/frozen/example.ts", "tests/fixtures/consumer/recipe/example_foundry.gen.go", `tests\fixtures\consumer\example.go`} {
		if _, err := readSource(root, name); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	alias := "tests/fixtures/consumer/recipe/alias.go"
	if err := os.Symlink(filepath.Join(root, good), filepath.Join(root, alias)); err != nil {
		t.Fatal(err)
	}
	if _, err := readSource(root, alias); err == nil {
		t.Fatal("accepted source alias")
	}
}

func TestCompilationInputs(t *testing.T) {
	root := t.TempDir()
	source := "tests/fixtures/consumer/recipe/example.go"
	docs := []Document{{Inclusions: []Inclusion{{Document: "guide.md", Line: 3, Source: source, Region: "read"}}}}
	for _, field := range []string{"GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles", "IgnoredGoFiles"} {
		data, _ := json.Marshal(map[string]any{"Dir": filepath.Join(root, "tests/fixtures/consumer/recipe"), field: []string{"example.go"}})
		err := CheckGoInputs(root, docs, strings.NewReader(string(data)))
		if (err != nil) != (field == "IgnoredGoFiles") {
			t.Fatalf("%s: %v", field, err)
		}
	}
	docs[0].Inclusions[0].Source = "tests/fixtures/consumer/testdata/ignored.go"
	if err := CheckGoInputs(root, docs, strings.NewReader("")); err == nil {
		t.Fatal("ignored testdata counted as proof")
	}
	docs[0].Inclusions[0].Source = "tests/fixtures/consumer/clientcontracts/testdata/example.ts"
	writeInput(t, root, TypeScriptInputs, `["example.ts"]`)
	if err := CheckTypeScriptInputs(root, docs); err != nil {
		t.Fatal(err)
	}
	writeInput(t, root, TypeScriptInputs, `["other.ts"]`)
	if err := CheckTypeScriptInputs(root, docs); err == nil {
		t.Fatal("unregistered TypeScript counted as proof")
	}
}

func TestEveryTeachingDocumentRequiresBindings(t *testing.T) {
	for _, name := range []string{"docs/guides/reference.md", "docs/guides/dated-20260101.md", "AGENTS.md", "database/AGENTS.md", ".claude/skills/example/SKILL.md", ".claude/skills/example/reference.md"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeInput(t, root, name, "# Guide\n```go\nvar x = 1\n```\n\n```tsx\nconst view = <Input />;\n```\n")
			writeInput(t, root, "docs/evidence/record.md", "```go\nvar historical = 1\n```\n")
			writeInput(t, root, "blueprint/design.md", "```go\nvar planned = 1\n```\n")
			docs, err := Load(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(docs) != 1 {
				t.Fatalf("loaded %d documents", len(docs))
			}
			err = CheckBindings(docs)
			if err == nil || !strings.Contains(err.Error(), name+":2: [doc-binding] Go block has no compiled source;") || !strings.Contains(err.Error(), name+":6: [doc-binding] TypeScript block has no compiled source;") {
				t.Fatalf("missing diagnostics: %v", err)
			}
		})
	}
}

func TestHiddenExecutableFences(t *testing.T) {
	for _, text := range []string{
		"```\npackage example\n```", "~~~text\nconst value = 1;\n~~~",
		"> ```typescript\n> const value = 1;\n> ```", "- ````go\n  var value = 1\n  ````",
		"```ts\nconst value = 1;\n```", "```tsx\nconst value = <Input />;\n```",
	} {
		doc, err := parseDocument(nil, "guide.md", []byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckBindings([]Document{doc}); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestOnlyStandaloneRegionMarkers(t *testing.T) {
	for _, text := range []string{
		"// doc-example:begin value\nvar Value = 1 // doc-example:end value\n",
		"type Settings struct { // doc-example:begin settings\n Name string\n} // doc-example:end settings\n",
		"// migration:value\nvar Value = 1\n// migration:end\n",
		"// guide:value\nvar Value = 1\n// guide:end\n",
	} {
		if _, err := parseSource(text, "go"); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestDiagnosticShape(t *testing.T) {
	err := CheckBindings([]Document{{BindingErrors: []error{
		diagnostic.DocBinding.At("guide.md", 7, 0).Text("", "Go block has no compiled source; bind it").Err(),
		diagnostic.DocFile.At("AGENTS.md", 0, 0).Say("input", "cannot be read").To("restore read access").Err(),
	}}})
	want := "guide.md:7: [doc-binding] Go block has no compiled source; bind it. Read docs/guides/developer-tooling-and-testing.md#recipe-author-examples\n" +
		"AGENTS.md: [doc-file] input: cannot be read; restore read access. Read docs/guides/developer-tooling-and-testing.md#pinned-documentation-checks"
	if got := Diagnostics(err); got != want {
		t.Fatalf("got %q", got)
	}
	got := Diagnostics(errors.New("open guide.md: permission denied"))
	if want := "guide.md: [doc-file] input: permission denied; restore the file and its required access permissions. Read docs/guides/developer-tooling-and-testing.md#pinned-documentation-checks"; got != want {
		t.Fatalf("bare filesystem failure = %q", got)
	}
}

func TestShellGoCommandsAreNotGoStatements(t *testing.T) {
	text := "```sh\ngo list -m -f '{{.Dir}}' example.test/framework\ngo get -tool \"example.test/framework@${VERSION:?select a version}\"\n```"
	doc, err := parseDocument(nil, "guide.md", []byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings([]Document{doc}); err != nil {
		t.Fatal(err)
	}
}
