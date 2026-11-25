package docrefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestReferenceDiagnosticsRetainTypedLocations(t *testing.T) {
	_, err := Parse(nil, "AGENTS.md", []byte("Foundry guides: bad\nFoundry guides: also-bad\n"))
	message, owned := safeerror.OwnedMessage(err)
	if !owned || !strings.Contains(message, "AGENTS.md:1:") || !strings.Contains(message, "AGENTS.md:2:") {
		t.Fatalf("lost captured diagnostics: %q", message)
	}
	found, other := diagnostic.Findings(err)
	if len(found) != 2 || len(other) != 0 || found[0].Path != "AGENTS.md" || found[0].Line != 1 || found[0].Rule != diagnostic.GuidesDeclaration {
		t.Fatalf("lost typed location: %+v, %v", found, err)
	}
	one := diagnostic.GuidesDeclaration.At("AGENTS.md", 3, 0).Say("Foundry guides:", "declaration is malformed").Err()
	if safeerror.Join("documentation failed", nil, one) != one {
		t.Fatal("singleton diagnostic identity lost")
	}
}

func TestReferenceGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		count      int
		bad        bool
	}{
		{"bare", "Foundry guides: `http-endpoints`", 1, false},
		{"fragments", " Foundry guides: `http-endpoints#agent-entry`, `orm`\nFoundry guides: `orm#recipe-2`", 3, false},
		{"regions", "<!-- app-owned:begin domain -->\nFoundry guides: `orm#models`\n<!-- app-owned:end domain -->", 1, false},
		{"fences", "````md\n```\nFoundry guides: `../bad`\n```\n````\n~~~md\nFoundry guides: bad\n~~~\nFoundry guides: `orm`", 1, false},
		{"fence tail", "```md\n``` not-a-close\nFoundry guides: bad\n```\n", 0, false},
		{"empty", "Foundry guides:", 0, true},
		{"unquoted", "Foundry guides: orm", 0, true},
		{"missing close", "Foundry guides: `orm", 0, true},
		{"partial", "Foundry guides: `orm`, `broken#`", 0, true},
		{"tail", "Foundry guides: `orm` and more", 0, true},
		{"trailing comma", "Foundry guides: `orm`,", 0, true},
		{"extra comma", "Foundry guides: `orm`,, `http`", 0, true},
		{"double ticks", "Foundry guides: ``orm``", 0, true},
		{"space", "Foundry guides: `orm guide`", 0, true},
		{"uppercase", "Foundry guides: `ORM`", 0, true},
		{"unicode", "Foundry guides: `ö`", 0, true},
		{"traversal", "Foundry guides: `../orm`", 0, true},
		{"encoded traversal", "Foundry guides: `%2e%2e%2form`", 0, true},
		{"slash", "Foundry guides: `http/orm`", 0, true},
		{"extension", "Foundry guides: `orm.md`", 0, true},
		{"fragment traversal", "Foundry guides: `orm#../entry`", 0, true},
		{"extra fragment", "Foundry guides: `orm#entry#next`", 0, true},
		{"edge hyphen", "Foundry guides: `-orm`", 0, true},
		{"internal hyphens", "Foundry guides: `orm#agent--entry`", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs, err := Parse(nil, "AGENTS.md", []byte(tc.text))
			if (err != nil) != tc.bad || len(refs) != tc.count {
				t.Fatalf("refs=%v err=%v", refs, err)
			}
			if err != nil && !strings.Contains(err.Error(), "AGENTS.md:1:") {
				t.Fatal(err)
			}
		})
	}
	refs, err := Parse(nil, "skills/recipe.md", []byte("intro\nFoundry guides: `orm#entry`\nFoundry guides: `http`"))
	if err != nil || refs[0] != (Reference{"skills/recipe.md", 2, "orm", "entry"}) || refs[1].Line != 3 {
		t.Fatalf("%v %v", refs, err)
	}
}

func TestExplicitAnchorIndexAndNavigation(t *testing.T) {
	data := []byte("# A heading is not an ID\n<a id=\"agent-entry\"></a>\n## Agent entry\n```html\n<a id=\"agent-entry\"></a>\n<a id='BAD'></a>\n```\n<a id=\"recipe-2\"></a>\n## Recipe\n[entry](#agent-entry)\nAn ID uses `<a id=\"example\"></a>` syntax.")
	index, err := IndexAnchors(nil, "orm.md", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"agent-entry", "recipe-2"} {
		if err := index.CheckFragment("orm.md", 10, "page", fragment); err != nil {
			t.Fatal(err)
		}
	}
	for _, fragment := range []string{"a-heading-is-not-an-id", "missing", "#agent-entry", "Agent-Entry", "%61gent-entry", ""} {
		if err := index.CheckFragment("orm.md", 10, "page", fragment); err == nil || !strings.Contains(err.Error(), "orm.md:10:") {
			t.Fatalf("fragment %q: %v", fragment, err)
		}
	}
	for _, anchor := range []string{`<a id="UPPER"></a>`, `<a id="-edge"></a>`, `<a id="edge-"></a>`, `<a id=""></a>`, `<a id='entry'></a>`, `<a id="entry" />`, `<a id="entry"></a> trailing`, `<a id="a/b"></a>`} {
		if _, err := IndexAnchors(nil, "guide.md", []byte("intro\n"+anchor)); err == nil || !strings.Contains(err.Error(), "guide.md:2:") {
			t.Fatalf("%s: %v", anchor, err)
		}
	}
	_, err = IndexAnchors(nil, "guide.md", []byte("<a id=\"entry\"></a>\n# Title\n<a id=\"entry\"></a>"))
	if err == nil || !strings.Contains(err.Error(), "guide.md:3:") || !strings.Contains(err.Error(), "line 1") {
		t.Fatal(err)
	}
}

func TestSelectedGuideResolution(t *testing.T) {
	dir := t.TempDir()
	put := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("orm.md", "<a id=\"entry\"></a>\n# Renamed heading\n")
	put("duplicate.md", "<a id=\"entry\"></a>\n<a id=\"entry\"></a>\n")
	for _, tc := range []struct{ guide, anchor, want string }{
		{"orm", "", ""}, {"orm", "entry", ""}, {"missing", "", "missing"},
		{"orm", "heading", "[anchor-missing] anchor \"heading\" of guide orm: not declared"}, {"duplicate", "", "[guide-missing] guide duplicate"}, {"../orm", "", "[guide-name]"},
	} {
		err := CheckReferences(dir, []Reference{{"AGENTS.md", 7, tc.guide, tc.anchor}})
		if tc.want == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "AGENTS.md:7:") {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "orm.md"), filepath.Join(dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := CheckReferences(dir, []Reference{{"AGENTS.md", 1, "linked", "entry"}}); err == nil {
		t.Fatal("followed guide symlink")
	}
	// GitHub's pinned tree and offline reading use the same literal page/ID.
	refs, err := Parse(nil, "AGENTS.md", []byte("Foundry guides: `orm#entry`"))
	if err != nil {
		t.Fatal(err)
	}
	ref := refs[0]
	url := "https://github.com/weiloon1234/Foundry-Go/tree/v0.11.0/docs/guides/" + ref.Guide + ".md#" + ref.Anchor
	if !strings.HasSuffix(url, "/orm.md#entry") {
		t.Fatal(url)
	}
	if err := CheckReferences(dir, refs); err != nil {
		t.Fatal(err)
	}
}

func TestParserContainersAndComments(t *testing.T) {
	input := "> ```text\n> Foundry guides: bad\n\nFoundry guides: `orm`\n\n<!--\nFoundry guides: bad\n<a id=\"hidden\"></a>\n-->\n\n> <a id=\"visible\"></a>\n"
	refs, err := Parse(nil, "AGENTS.md", []byte(input))
	if err != nil || len(refs) != 1 || refs[0].Line != 4 {
		t.Fatalf("references: %+v %v", refs, err)
	}
	anchors, err := IndexAnchors(nil, "AGENTS.md", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if err := anchors.CheckFragment("AGENTS.md", 12, "page", "visible"); err != nil {
		t.Fatal(err)
	}
	if err := anchors.CheckFragment("AGENTS.md", 12, "page", "hidden"); err == nil {
		t.Fatal("comment became anchor")
	}
}

func TestAnchorTargetIsIndexedOncePerRun(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "guide.md")
	if err := os.WriteFile(target, []byte("<a id=\"recipe-first\"></a>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pages := NewPages(root, nil)
	first, err := pages.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("<a id=\"recipe-second\"></a>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := pages.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.CheckFragment("source.md", 1, "page", "recipe-first"); err != nil {
		t.Fatal(err)
	}
	if err := first.CheckFragment("source.md", 1, "page", "recipe-second"); err == nil {
		t.Fatal("cached target reread")
	}
	fresh, err := NewPages(root, nil).Load(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.CheckFragment("source.md", 1, "page", "recipe-second"); err != nil {
		t.Fatal(err)
	}
}

func TestInlineCommentCannotHideMalformedDeclaration(t *testing.T) {
	for _, input := range []string{"Foundry guides: bad <!-- comment -->\n", "Foundry guides: bad <!-- multiline\ncomment -->\n"} {
		if _, err := Parse(nil, "AGENTS.md", []byte(input)); err == nil {
			t.Fatalf("comment hid declaration: %q", input)
		}
	}
	refs, err := Parse(nil, "AGENTS.md", []byte("# Foundry guides: bad\n"))
	if err != nil || len(refs) != 0 {
		t.Fatalf("heading became declaration: %v %v", refs, err)
	}
}

// Declarations are read from paragraph text wherever Markdown nests it. Code,
// raw HTML, comments, headings and table cells are not declarations.
func TestParseReadsDeclarationsFromParagraphTextOnly(t *testing.T) {
	for name, test := range map[string]struct {
		text string
		want int
	}{
		"paragraph":          {"Foundry guides: `orm`\n", 1},
		"list item":          {"- Foundry guides: `orm`\n", 1},
		"quote":              {"> Foundry guides: `orm`\n", 1},
		"after a comment":    {"<!-- c --> Foundry guides: `orm`\n", 0},
		"inside a comment":   {"<!-- Foundry guides: `orm` -->\n", 0},
		"multi-line comment": {"<!--\nFoundry guides: `orm`\n-->\n", 0},
		"heading":            {"# Foundry guides: `orm`\n", 0},
		"indented block":     {"    Foundry guides: `orm`\n", 0},
		"table cell":         {"| a |\n| --- |\n| Foundry guides: `orm` |\n", 0},
		"fence":              {"```\nFoundry guides: `orm`\n```\n", 0},
		"quoted fence":       {"> ```\n> Foundry guides: `orm`\n> ```\n", 0},
		"raw HTML block":     {"<div>\nFoundry guides: `orm`\n</div>\n", 0},
	} {
		t.Run(name, func(t *testing.T) {
			refs, err := Parse(nil, "AGENTS.md", []byte(test.text))
			if err != nil || len(refs) != test.want {
				t.Fatalf("refs = %v, err = %v; want %d", refs, err, test.want)
			}
		})
	}
}

func TestPagesIndexUsesRepositoryRelativeNamesForTheSharedCache(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "docs", "guides", "a.md")
	pages := NewPages(root, markdown.NewCache())
	data := []byte("<a id=\"entry\"></a>\n# T\n<a id=\"entry\"></a>\n")
	_, err := pages.Index(name, data)
	if err == nil || !strings.Contains(err.Error(), "docs/guides/a.md:3: [anchor-duplicate] anchor \"entry\": declared twice") || strings.Contains(err.Error(), root) {
		t.Fatalf("diagnostic should name the repository-relative path: %v", err)
	}
	if _, again := pages.Index(name, data); again == nil || again.Error() != err.Error() {
		t.Fatalf("second index of the same page changed: %v", again)
	}
}
