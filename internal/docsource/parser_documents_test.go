package docsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

func TestMarkdownContainerBoundaries(t *testing.T) {
	input := "> ```sh\n> echo ok\n\n```go\nvar Value = 1\n```\n"
	doc, err := parseDocument("guide.md", []byte(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckBindings([]Document{doc}); err == nil || !strings.Contains(err.Error(), "guide.md:4: Go block has no compiled source") {
		t.Fatalf("container swallowed executable fence: %v", err)
	}
	for _, input := range []string{
		"<!-- compiled-source: example.go -->\n> ```go\n> var Value = 1\n> ```\n",
		"> <!-- compiled-source: example.go -->\n\n```go\nvar Value = 1\n```\n",
		"<!-- compiled-source: example.go -->\nIntervening paragraph.\n```go\nvar Value = 1\n```\n",
	} {
		_, err := parseDocument("guide.md", []byte(input), nil)
		if err == nil || !strings.Contains(err.Error(), "immediately precede") {
			t.Fatalf("cross-container binding accepted: %v", err)
		}
	}
}

func TestMarkdownProseCommentsAndTables(t *testing.T) {
	for _, input := range []string{
		"- Parent\n  - Child\n    continuation prose.\n",
		"- Parent\n\t- Child\n\t  continuation prose.\n",
		"<!-- <pre> reserved for a later illustration </pre> -->\n",
		"<!--\n```go\nvar Value = 1\n```\n    apparent indentation\n<code>hidden</code>\n-->\n",
		"Name | Meaning\n--- | ---\n```literal``` | ordinary table text\n",
	} {
		doc, err := parseDocument("guide.md", []byte(input), nil)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if err = CheckBindings([]Document{doc}); err != nil {
			t.Fatalf("%q: %v", input, err)
		}
	}
	src, _ := parseSource("var Value = 1\n", "go")
	input := "- Parent\n  - Child\n\n    <!-- compiled-source: example.go -->\n    ```go\n    stale\n    ```\n"
	doc, err := parseDocument("guide.md", []byte(input), func(string) (source, error) { return src, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckBindings([]Document{doc}); err != nil {
		t.Fatal(err)
	}
	if string(doc.Rendered) != strings.Replace(input, "stale", "var Value = 1", 1) {
		t.Fatalf("changed outside body: %q", doc.Rendered)
	}
}

func TestTeachingInputSafety(t *testing.T) {
	for _, ancestor := range []string{"docs", ".claude"} {
		t.Run(ancestor, func(t *testing.T) {
			root := t.TempDir()
			external := t.TempDir()
			if err := os.Symlink(external, filepath.Join(root, ancestor)); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("accepted ancestor: %v", err)
			}
		})
	}
	for _, name := range []string{"AGENTS.md", "docs/guides/pipe.md", ".claude/skills/example/SKILL.md"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(file, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("accepted FIFO: %v", err)
			}
		})
	}
}

func TestBoundedInputs(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int
		load  func(string) error
	}{
		{"docs/guides/large.md", markdown.MaxDocumentBytes, func(root string) error { _, err := Load(root); return err }},
		{"tests/fixtures/consumer/large.go", markdown.MaxSourceBytes, func(root string) error { _, err := readSource(root, "tests/fixtures/consumer/large.go"); return err }},
		{TypeScriptInputs, markdown.MaxManifestBytes, func(root string) error {
			return CheckTypeScriptInputs(root, []Document{{Inclusions: []Inclusion{{Source: "tests/fixtures/consumer/clientcontracts/testdata/example.ts"}}}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeInput(t, root, test.name, strings.Repeat(" ", test.limit+1))
			err := test.load(root)
			if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
				t.Fatalf("unbounded input: %v", err)
			}
		})
	}
	err := CheckGoInputs(t.TempDir(), nil, strings.NewReader(strings.Repeat(" ", markdown.MaxManifestBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("unbounded stream: %v", err)
	}
}

type privateReadFailure struct{}

func (privateReadFailure) Error() string { return "PRIVATE-MARKER\ninjected diagnostic" }

type failingInput struct{ err error }

func (r failingInput) Read([]byte) (int, error) { return 0, r.err }
func TestCompilerInputReadFailureIsPrivate(t *testing.T) {
	cause := privateReadFailure{}
	err := CheckGoInputs(t.TempDir(), nil, failingInput{cause})
	if err == nil || !errors.Is(err, cause) {
		t.Fatalf("lost cause: %v", err)
	}
	if message := Diagnostics(err); strings.Contains(message, "PRIVATE-MARKER") || strings.Contains(message, "injected") {
		t.Fatalf("reader error leaked: %s", message)
	}
	if !strings.Contains(Diagnostics(err), "cannot read input") {
		t.Fatal(err)
	}
}

func TestBoundSourceCannotChangeFenceSyntax(t *testing.T) {
	for _, test := range []struct{ tag, body, want string }{
		{"go", "const value = 1;\n", "language differs"},
		{"typescript", "```\nconst value = 1;\n", "choose a longer fence"},
	} {
		src, _ := parseSource(test.body, "typescript")
		input := "<!-- compiled-source: example.ts -->\n```" + test.tag + "\nstale\n```\n"
		if _, err := parseDocument("guide.md", []byte(input), func(string) (source, error) { return src, nil }); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("rewrote fence syntax: %v", err)
		}
	}
}

func TestEveryInputReaderRequiresRegularFiles(t *testing.T) {
	for _, name := range []string{"example.md", "example.go", "inputs.json"} {
		root := t.TempDir()
		file := filepath.Join(root, name)
		if err := syscall.Mkfifo(file, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := markdown.ReadFile(root, name, markdown.MaxManifestBytes); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("read nonregular %s: %v", name, err)
		}
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/guides/example.md"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("accepted directory guide: %v", err)
	}
}

func TestRenderedDocumentIsBounded(t *testing.T) {
	src, _ := parseSource(strings.Repeat("x", markdown.MaxDocumentBytes), "go")
	input := "<!-- compiled-source: example.go -->\n```go\nstale\n```\n"
	_, err := parseDocument("guide.md", []byte(input), func(string) (source, error) { return src, nil })
	if err == nil || !strings.Contains(err.Error(), "rendered document exceeds maximum") {
		t.Fatalf("unbounded rendering: %v", err)
	}
}
