package docsource

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClosedBlockSyntax(t *testing.T) {
	tests := []struct{ name, block, fix string }{
		{"assignment without tag", "```\namount = 2\n```", "tag the block"},
		{"generic call without tag", "```\nclient.send<Request>(request);\n```", "tag the block"},
		{"attribute tag", "```{.typescript}\nclient.send<Request>(request);\n```", "tag the block"},
		{"unknown tag", "```js\namount = 2\n```", "tag the block"},
		{"tag attributes", "```typescript title=example\namount = 2\n```", "tag the block"},
		{"quoted text", "> ~~~text\n> const x: number = 1;\n> ~~~", "compiled source"},
		{"listed quoted text", "- > ~~~text\n  > const x: number = 1;\n  > ~~~", "compiled source"},
		{"quoted indentation", ">     const x: number = 1;", "use a fenced block"},
		{"plain indentation", "    output only", "use a fenced block"},
		{"tab indentation", "\toutput only", "use a fenced block"},
		{"pre", "<pre>\nconst x: number = 1;\n</pre>", "use a fenced block"},
		{"code", "<code>\namount = 2\n</code>", "use a fenced block"},
		{"quoted html", "> <PRE class=example>\n> output\n> </PRE>", "use a fenced block"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parseDocument(nil, "guide.md", []byte("# Example\n\n"+test.block+"\n"), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = CheckBindings([]Document{doc})
			if err == nil || !strings.Contains(err.Error(), "guide.md:3:") || !strings.Contains(err.Error(), test.fix) {
				t.Fatalf("missing located repair: %v", err)
			}
		})
	}
	for _, tag := range []string{"sh", "toml", "json", "sql", "text", "ini", "markdown", "yaml", "makefile"} {
		if executable(tag) {
			continue
		}
		doc, err := parseDocument(nil, "guide.md", []byte("```"+tag+"\nplain output\n```\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = CheckBindings([]Document{doc}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOwnedTreesHaveNoDirectoryNameExemptions(t *testing.T) {
	for _, rootName := range []string{"docs/guides", ".claude/skills"} {
		for _, dir := range []string{"vendor", "node_modules", ".cache", ".deploy", ".git"} {
			t.Run(rootName+"/"+dir, func(t *testing.T) {
				root := t.TempDir()
				name := rootName + "/" + dir + "/example.md"
				writeInput(t, root, name, "```go\nvar Value = 1\n```\n")
				docs, err := Load(root, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = CheckBindings(docs); err == nil || !strings.Contains(err.Error(), name+":1:") {
					t.Fatalf("directory escaped: %v", err)
				}
			})
		}
		t.Run(rootName+"/symlink", func(t *testing.T) {
			root := t.TempDir()
			target := t.TempDir()
			name := filepath.Join(root, rootName, "linked")
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, name); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root, nil); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("directory symlink accepted: %v", err)
			}
		})
	}
}

func TestContainerRendering(t *testing.T) {
	src, err := parseSource("// doc-example:begin value\nconst value = 1;\n// doc-example:end value\n", "typescript")
	if err != nil {
		t.Fatal(err)
	}
	for _, container := range []struct{ prefix, continuation string }{{"> ", "> "}, {"- ", "  "}, {"> - ", ">   "}, {"1. ", "   "}} {
		prefix, continuation := container.prefix, container.continuation
		input := prefix + "<!-- compiled-source: example.ts#value -->\n" + continuation + "```typescript\n" + continuation + "const value = 1;\n" + continuation + "```\n"
		doc, err := parseDocument(nil, "guide.md", []byte(input), func(string) (source, error) { return src, nil })
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckBindings([]Document{doc}); err != nil {
			t.Fatal(err)
		}
		if string(doc.Rendered) != input {
			t.Fatalf("container changed: %q", doc.Rendered)
		}
	}
}

func TestSourceDiagnosticKeepsUnclosedRegionLine(t *testing.T) {
	root := t.TempDir()
	name := "tests/fixtures/consumer/example/example.go"
	writeInput(t, root, name, "package example\n\n// doc-example:begin value\nvar Value = 1\n")
	writeInput(t, root, "docs/guides/example.md", "<!-- compiled-source: "+name+"#value -->\n```go\n```\n")
	_, err := Load(root, nil)
	if err == nil || !strings.Contains(Diagnostics(err), name+":3: [doc-region] unclosed region value; add its matching end marker.") {
		t.Fatalf("lost source location: %v", err)
	}
}

func TestArgumentDiagnostics(t *testing.T) {
	for _, command := range []string{"checkdocs", "renderdocs"} {
		for _, args := range [][]string{{"extra"}, {"--unknown"}, {"--root"}} {
			flags := flag.NewFlagSet(command, flag.ContinueOnError)
			flags.String("root", ".", "root")
			err := ParseFlags(flags, args)
			if err == nil || !strings.HasPrefix(Diagnostics(err), command+": ") || !strings.Contains(Diagnostics(err), "; ") {
				t.Fatalf("argument diagnostic: %v", err)
			}
		}
	}
}

func TestManifestDiagnosticsKeepLines(t *testing.T) {
	root := t.TempDir()
	writeInput(t, root, TypeScriptInputs, "[\n  \"example.ts\",\n  \"example.ts\"\n]\n")
	docs := []Document{{Inclusions: []Inclusion{{Source: "tests/fixtures/consumer/clientcontracts/testdata/example.ts"}}}}
	err := CheckTypeScriptInputs(root, docs)
	if err == nil || !strings.Contains(err.Error(), TypeScriptInputs+`:3: [doc-inputs] compiler input "example.ts": listed twice`) || !strings.Contains(err.Error(), "remove the duplicate") {
		t.Fatalf("lost manifest location: %v", err)
	}
}

func TestLiteralHTMLNamesAreNotBlocks(t *testing.T) {
	for _, text := range []string{"Use `http.error.<code>` keys.", "The ``literal ` <pre> `` is an inline example."} {
		doc, err := parseDocument(nil, "guide.md", []byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckBindings([]Document{doc}); err != nil {
			t.Fatalf("inline literal treated as HTML: %v", err)
		}
	}
	doc, err := parseDocument(nil, "guide.md", []byte("Use `<code>` literally. <pre>actual HTML</pre>"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings([]Document{doc}); err == nil || !strings.Contains(err.Error(), "use backticks") {
		t.Fatalf("inline literal hid real HTML: %v", err)
	}
}

func TestMarkerFollowedByTextMustStandAlone(t *testing.T) {
	doc, err := parseDocument(nil, "guide.md", []byte("# T\n\n<!-- compiled-source: a.go#r --> text\n```go\nx := 1\n```\n"), nil)
	if err == nil || !strings.Contains(err.Error(), "guide.md:3: [doc-marker] compiled-source marker must stand alone on its line") || strings.Contains(err.Error(), "no compiled source") {
		t.Fatalf("want the stand-alone message, got %v (%v)", err, doc)
	}
}
