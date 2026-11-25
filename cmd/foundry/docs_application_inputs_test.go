package main

import (
	"strings"
	"testing"
)

// These are the application-visible rules of `foundry docs check` that the
// release notes list; each is observed through the real command path.
func TestDocsApplicationInputRules(t *testing.T) {
	const retired = "http.Endpoint.WithScopes"
	tests := []struct {
		name, file, content, want string
	}{
		{"oversize document", "AGENTS.md", "Foundry guides: `orm`\n" + strings.Repeat("Ordinary text line.\n", 30000), "AGENTS.md:0: input exceeds maximum 524288 bytes"},
		{"CR-only line endings", "AGENTS.md", "# T\r\rFoundry guides: `orm`\r", "AGENTS.md:1: document uses CR-only line endings"},
		{"line over the limit", "AGENTS.md", strings.Repeat("a", 20000) + "\n", "AGENTS.md:1: line exceeds maximum 16384 bytes"},
		{"nesting over the limit", "AGENTS.md", strings.Repeat("> ", 17) + "x\n", "AGENTS.md:1: line opens with more than 16 quote or list markers"},
		{"secret path component", ".claude/skills/.env.d/policy.md", "Foundry guides: `orm`\n", ".claude/skills/.env.d/policy.md:0: secret inputs cannot be read as documentation"},
		{"indented block is scanned", "AGENTS.md", "Example:\n\n    " + retired + "(endpoint)\n", "AGENTS.md:3: " + retired + " is retired;"},
		{"table cell is scanned", "AGENTS.md", "| Name | Use |\n| --- | --- |\n| scope | `" + retired + "` |\n", "AGENTS.md:3: " + retired + " is retired;"},
		{"quoted fence is scanned", "AGENTS.md", "> ```go\n> " + retired + "(endpoint)\n> ```\n", "AGENTS.md:2: " + retired + " is retired;"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, _, goBinary, build := documentationFixture(t, "match")
			putDocumentation(t, app, test.file, test.content)
			err := checkDocs(t.Context(), app, goBinary, build)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestDocsApplicationIgnoresRetiredTextInsideComments(t *testing.T) {
	app, _, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#agent-entry`\n<!-- Use http.Endpoint.WithScopes(endpoint) -->\n<!-- a\nhttp.Endpoint.WithScopes\nb -->\n")
	if err := checkDocs(t.Context(), app, goBinary, build); err != nil {
		t.Fatal(err)
	}
	// Text after the comment on its line is documentation like any other.
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#agent-entry`\n<!-- note --> Use http.Endpoint.WithScopes(endpoint)\n")
	err := checkDocs(t.Context(), app, goBinary, build)
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md:2: http.Endpoint.WithScopes is retired;") {
		t.Fatalf("text after a comment was not checked: %v", err)
	}
}
