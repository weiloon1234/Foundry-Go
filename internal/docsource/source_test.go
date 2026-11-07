package docsource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentNavigationLines(t *testing.T) {
	for _, heading := range []string{"## Agent entry", "<a id=\"agent-entry\"></a>\n## Tasks"} {
		text := heading + "\n\n- Read: [Read](#recipe-read)\n\n## Background\n\n[Other](#other)\n"
		document, err := parseDocument("guide.md", []byte(text), nil)
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
		_, err := parseDocument("guide.md", []byte("<a id=\"recipe-read\"></a>\n"+tail), nil)
		if err == nil || !strings.Contains(err.Error(), "must be followed by one ATX heading") {
			t.Fatalf("displaced anchor %q: %v", tail, err)
		}
	}
	if _, err := parseDocument("guide.md", []byte("<a id=\"recipe-read\"></a>\n\n## Read\n"), nil); err != nil {
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
			if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "actual line comment") {
				t.Fatalf("literal marker accepted as compiler proof: %v", err)
			}
		})
	}
}

func TestSourceCommentContexts(t *testing.T) {
	for _, language := range []string{"go", "typescript"} {
		text := "const value = \"/* not a comment */\"\n// doc-example:begin value\nvar result = value // doc-example:end value\n"
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
	for _, marker := range []string{"<!-- go-source: example.go -->", "<!-- compiled-examples -->"} {
		if _, err := parseDocument("guide.md", []byte(marker+"\n"), nil); err == nil || !strings.Contains(err.Error(), "obsolete guide snippet marker") {
			t.Fatalf("accepted obsolete marker %q: %v", marker, err)
		}
		if _, err := parseDocument("guide.md", []byte("```markdown\n"+marker+"\n```\n"), nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderPreservesProseAndIsIdempotent(t *testing.T) {
	original := "# Guide\r\n\r\nAuthored prose.\r\n<!-- compiled-source: example.ts#example -->\r\n\r\n~~~go\nstale\n~~~\n\nAfter.\n"
	src, err := parseSource("const before = 0;\n// doc-example:begin example\n  const fence = \" ``` \";\n  const value = 1;\n// doc-example:end example\n", "typescript")
	if err != nil {
		t.Fatal(err)
	}
	read := func(string) (source, error) { return src, nil }
	first, err := parseDocument("guide.md", []byte(original), read)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "~~~go\nstale\n~~~", "````typescript\nconst fence = \" ``` \";\nconst value = 1;\n````", 1)
	if string(first.Rendered) != want {
		t.Fatalf("got %q, want %q", first.Rendered, want)
	}
	if err := Check([]Document{first}); err == nil {
		t.Fatal("edited fence passed equality")
	}
	second, err := parseDocument("guide.md", first.Rendered, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check([]Document{second}); err != nil {
		t.Fatal(err)
	}
	src.regions["example"] = excerpt{text: "changed\n"}
	changed, err := parseDocument("guide.md", first.Rendered, read)
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
		{"recipe sibling", "<a id=\"recipe-read\"></a>\n## Read\n## Reference\n```go\nx\n```", false},
		{"quoted recipe", "<a id=\"recipe-read\"></a>\n## Read\n> ```go\nx\n> ```", true},
		{"listed recipe", "<a id=\"recipe-read\"></a>\n## Read\n- ```typescript\nx\n  ```", true},
		{"typescript alias", "<a id=\"recipe-read\"></a>\n## Read\n```ts\nx\n```", true},
		{"nonexecutable", "<a id=\"recipe-read\"></a>\n## Read\n```json\n{}\n```", false},
		{"agent link", "<a id=\"agent-entry\"></a>\n## Agent entry\n- Read: [Read](#recipe-read)\n", false},
		{"agent prose", "## Agent entry\nUse this API.\n", true},
		{"agent fence", "## Agent entry\n```sh\ncommand\n```", true},
		{"syntax in fence", "```md\n<!-- compiled-source: absent.go -->\n<a id=\"recipe-read\"></a>\n```\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseDocument("guide.md", []byte(test.text), read)
			if (err != nil) != test.bad {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSingleOwningInclusion(t *testing.T) {
	owner := Inclusion{Document: "one.md", Source: "example.go", Region: "one", First: 2, Last: 5}
	for _, test := range []struct {
		region      string
		first, last int
		bad         bool
	}{
		{"one", 2, 5, true}, {"", 1, 10, true}, {"two", 4, 7, true}, {"two", 6, 8, false},
	} {
		other := Inclusion{Document: "two.md", Source: "example.go", Region: test.region, First: test.first, Last: test.last}
		if err := uniqueOwners([]Inclusion{owner, other}); (err != nil) != test.bad {
			t.Fatalf("%+v: %v", test, err)
		}
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

func TestCompleteGuidesRetainUnlinkedFenceRejection(t *testing.T) {
	read := func(string) (source, error) { return source{}, nil }
	for name := range completeGuides {
		if _, err := parseDocument("docs/guides/"+name, []byte("# Guide\n```go\nvar x = 1\n```\n"), read); err == nil {
			t.Fatalf("%s accepted an unbound Go fence", name)
		}
	}
	if _, err := parseDocument("docs/guides/reference.md", []byte("# Guide\n```go\nvar x = 1\n```\n"), read); err != nil {
		t.Fatal(err)
	}
}

func TestTrailingEndMarkerPreservesSourceLine(t *testing.T) {
	source, err := parseSource("package example\n// doc-example:begin value\nvar Value = 1 // doc-example:end value\n\nvar Other = 2\n", "go")
	if err != nil {
		t.Fatal(err)
	}
	if got := source.regions["value"].text; got != "var Value = 1\n" {
		t.Fatalf("got %q", got)
	}
	if got := source.whole.text; got != "package example\nvar Value = 1\n\nvar Other = 2\n" {
		t.Fatalf("whole file %q", got)
	}
}

func TestTrailingBeginLeavesGoDeclarationDirectiveOutsideExcerpt(t *testing.T) {
	source, err := parseSource("//foundry:config\ntype Settings struct { // doc-example:begin settings\n Name string\n} // doc-example:end settings\n", "go")
	if err != nil {
		t.Fatal(err)
	}
	if got := source.regions["settings"].text; got != "type Settings struct {\n Name string\n}\n" {
		t.Fatalf("got %q", got)
	}
	if !strings.HasPrefix(source.whole.text, "//foundry:config\ntype Settings") {
		t.Fatal("whole file lost its declaration directive")
	}
}
