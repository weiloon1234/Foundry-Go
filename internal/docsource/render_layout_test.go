package docsource

import (
	"strings"
	"testing"
)

func regionReader(t *testing.T, text string) func(string) (source, error) {
	t.Helper()
	src, err := parseSource("// doc-example:begin r\n"+text+"// doc-example:end r\n", "go")
	if err != nil {
		t.Fatal(err)
	}
	return func(string) (source, error) { return src, nil }
}

func TestRenderKeepsDocumentLayoutAndIsIdempotent(t *testing.T) {
	const marker = "<!-- compiled-source: a.go#r -->"
	tests := []struct {
		name, original, want string
	}{
		{
			name:     "CRLF document",
			original: "# G\r\n\r\n" + marker + "\r\n```go\r\nold\r\n```\r\n",
			want:     "# G\r\n\r\n" + marker + "\r\n```go\r\nfirst\r\n\r\nsecond\r\n```\r\n",
		},
		{
			name:     "fence inside a list item",
			original: "- item\n\n  " + marker + "\n  ```go\n  old\n  ```\n",
			want:     "- item\n\n  " + marker + "\n  ```go\n  first\n\n  second\n  ```\n",
		},
		{
			name:     "fence inside a quote",
			original: "> " + marker + "\n> ```go\n> old\n> ```\n",
			want:     "> " + marker + "\n> ```go\n> first\n>\n> second\n> ```\n",
		},
		{
			name:     "fence opening a list item",
			original: "1. " + marker + "\n   ```go\n   old\n   ```\n",
			want:     "1. " + marker + "\n   ```go\n   first\n\n   second\n   ```\n",
		},
		{
			name:     "closing fence indented more than the opener",
			original: "- " + marker + "\n  ```go\n  old\n   ```\n",
			want:     "- " + marker + "\n  ```go\n  first\n\n  second\n   ```\n",
		},
		{
			name:     "quote in list",
			original: "- item\n\n  > " + marker + "\n  > ```go\n  > old\n  > ```\n",
			want:     "- item\n\n  > " + marker + "\n  > ```go\n  > first\n  >\n  > second\n  > ```\n",
		},
		{
			name:     "no final newline",
			original: marker + "\n```go\nold\n```",
			want:     marker + "\n```go\nfirst\n\nsecond\n```",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			read := regionReader(t, "first\n\nsecond\n")
			first, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte(test.original), read)
			if err != nil {
				t.Fatal(err)
			}
			if string(first.Rendered) != test.want {
				t.Fatalf("rendered %q, want %q", first.Rendered, test.want)
			}
			if strings.Contains(string(first.Rendered), " \n") || strings.Contains(string(first.Rendered), " \r\n") {
				t.Fatalf("rendered trailing whitespace in %q", first.Rendered)
			}
			second, err := parseDocument(FrameworkPolicy(), nil, "guide.md", first.Rendered, read)
			if err != nil {
				t.Fatal(err)
			}
			if string(second.Rendered) != string(first.Rendered) || len(second.StaleLines) != 0 {
				t.Fatalf("second render changed the document: %q (stale %v)", second.Rendered, second.StaleLines)
			}
			if err := Check([]Document{second}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommentDoesNotHideWhatFollowsIt(t *testing.T) {
	for name, text := range map[string]string{
		"element after comment":           "<!-- c --><pre>x</pre>\n",
		"spaced element after comment":    "<!-- c --> <pre>x</pre>\n",
		"opener only after comment":       "<!-- c --> <code>\n",
		"multi-line comment then element": "<!-- a\nb --><pre>x</pre>\n",
		"element before comment":          "<pre>x</pre> <!-- c -->\n",
		"after a document comment block":  "# T\n\n<!-- c -->\n\n<!-- d --><code>x</code>\n",
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte(text), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = CheckBindings([]Document{doc})
			if err == nil || !strings.Contains(err.Error(), "guide.md:") || !strings.Contains(err.Error(), "pre/code elements are unsupported") {
				t.Fatalf("hidden element accepted: %v", err)
			}
		})
	}
	for name, text := range map[string]string{
		"comment holding an element":            "<!-- <pre>x</pre> -->\n",
		"multi-line comment holding an element": "<!-- a\n<code>b</code>\nc -->\n",
		"two comments":                          "<!-- a --> <!-- b -->\n",
		"comment then prose":                    "<!-- c --> plain text\n",
	} {
		doc, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte(text), nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := CheckBindings([]Document{doc}); err != nil {
			t.Errorf("%s: comment contents were treated as markup: %v", name, err)
		}
	}
	// The element's own line is reported, not the comment's first line.
	doc, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte("# T\n\n<!-- a\nb --><pre>x</pre>\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBindings([]Document{doc}); err == nil || !strings.Contains(err.Error(), "guide.md:3:") {
		t.Fatalf("diagnostic not located at the block: %v", err)
	}
}

func TestInlineHTMLCodeInProseIsOneDiagnosticAboutBackticks(t *testing.T) {
	doc, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte("Use <code>x</code> and <pre>y</pre> here.\n\nSecond <code>z</code> line.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.BindingErrors) != 2 {
		t.Fatalf("want one diagnostic per line, got %d: %v", len(doc.BindingErrors), doc.BindingErrors)
	}
	for i, line := range []string{"guide.md:1:", "guide.md:3:"} {
		message := doc.BindingErrors[i].Error()
		if !strings.Contains(message, line) || !strings.Contains(message, "use backticks") || strings.Contains(message, "HTML code blocks") {
			t.Errorf("diagnostic %d = %q", i, message)
		}
	}
}

func TestLoneCarriageReturnDocumentIsRefused(t *testing.T) {
	_, err := parseDocument(FrameworkPolicy(), nil, "guide.md", []byte("# T\r\r```go\rx := 1\r```\r"), nil)
	if err == nil || !strings.Contains(err.Error(), "guide.md:1: [doc-limit] line ending: a lone CR") {
		t.Fatalf("CR-only document accepted: %v", err)
	}
}
