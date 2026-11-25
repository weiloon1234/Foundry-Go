package docsource

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
)

func TestIndentationColumnsAndHTMLOpenings(t *testing.T) {
	for _, body := range []string{
		" \tconst x: number = 1;", "  \toutput", "   \toutput", ">  \toutput",
		"<pre/>const x: number = 1;</pre>", "<code/>amount = 2</code>",
		"<pre class=example/>output</pre>", "<code class=example>output</code>",
		"<PRE data-language='go'>output</PRE>", "<code\nclass=example>output</code>",
	} {
		doc, err := parseDocument("guide.md", []byte("# Guide\n\n"+body+"\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckBindings([]Document{doc})
		if err == nil || !strings.Contains(err.Error(), "guide.md:3:") || !strings.Contains(err.Error(), "use a fenced block") {
			t.Fatalf("accepted block %q: %v", body, err)
		}
	}
	doc, err := parseDocument("guide.md", []byte("# Guide\n\n   Three-space prose.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckBindings([]Document{doc}); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownExecutableContent(t *testing.T) {
	for _, body := range []string{
		"package demo\nfunc Example() {}\n", "const x: number = 1;\n",
		"```go\namount = 2\n```\n", "~~~typescript\nclient.send<Request>(request);\n~~~\n",
		"> ```tsx\n> value\n> ```\n", "```go\n \n~~~\n",
	} {
		doc, err := parseDocument("guide.md", []byte("````markdown\n"+body+"````\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckBindings([]Document{doc})
		if err == nil || !strings.Contains(err.Error(), "compiled source") {
			t.Fatalf("Markdown hid executable content %q: %v", body, err)
		}
	}
	for _, body := range []string{"[Read](guide.md)\n", "<!-- compiled-source: tests/fixtures/consumer/example/query.go#read -->\n```go\n```\n"} {
		doc, err := parseDocument("guide.md", []byte("````markdown\n"+body+"````\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = CheckBindings([]Document{doc}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReferencedAnchorDiagnosticLeadsWithSource(t *testing.T) {
	root := t.TempDir()
	writeInput(t, root, "orm.md", "# ORM\n")
	err := docrefs.CheckReferences(root, []docrefs.Reference{{File: "AGENTS.md", Line: 7, Guide: "orm", Anchor: "recipe-missing"}})
	if err == nil || !strings.HasPrefix(Diagnostics(err), "AGENTS.md:7: explicit anchor") || strings.Contains(Diagnostics(err), "guide orm:0") {
		t.Fatalf("location lost: %v", err)
	}
}

func TestJSONInputErrorLines(t *testing.T) {
	for _, input := range []string{"[\n  \"example.ts\",\n  17\n]\n", "[\n  \"example.ts\",\n  ?\n]\n"} {
		root := t.TempDir()
		writeInput(t, root, TypeScriptInputs, input)
		docs := []Document{{Inclusions: []Inclusion{{Source: "tests/fixtures/consumer/clientcontracts/testdata/example.ts"}}}}
		err := CheckTypeScriptInputs(root, docs)
		if err == nil || !strings.HasPrefix(Diagnostics(err), TypeScriptInputs+":3: invalid compiler input JSON:") {
			t.Fatalf("JSON location lost: %v", err)
		}
	}
}

func TestRegionCommentBoundaries(t *testing.T) {
	for _, language := range []string{"go", "typescript"} {
		for _, body := range []string{"//\n//foundry:dto\ntype Example struct{}\n", "// Whole comment.\n//\n", "\n\t//\nvalue\n\n", "value\n \t//\n\n"} {
			_, err := parseSource("// doc-example:begin example\n"+body+"// doc-example:end example\n", language)
			if err == nil || !strings.Contains(err.Error(), "move the region marker outside the comment group, or include the whole comment") {
				t.Fatalf("accepted split comment group in %s: %q: %v", language, body, err)
			}
		}
		for _, body := range []string{"// Whole comment.\n//\n//foundry:dto\ntype Example struct{}\n", "\n// Whole comment.\n\n", "value\n// Interior separator.\n//\nnext\n", "\n"} {
			if _, err := parseSource("// doc-example:begin example\n"+body+"// doc-example:end example\n", language); err != nil {
				t.Fatal(err)
			}
		}
	}
	root := t.TempDir()
	sourcePath := "tests/fixtures/consumer/example/example.go"
	writeInput(t, root, sourcePath, "package example\n\n// doc-example:begin example\n\n//\nvar Value = 1\n// doc-example:end example\n")
	writeInput(t, root, "docs/guides/example.md", "<!-- compiled-source: "+sourcePath+"#example -->\n```go\n```\n")
	_, err := Load(root)
	if err == nil || !strings.HasPrefix(err.Error(), sourcePath+":5:") {
		t.Fatalf("boundary lost source location: %v", err)
	}
}
