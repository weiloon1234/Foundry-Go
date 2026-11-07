package docrefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestReferenceDiagnosticsRetainTypedLocations(t *testing.T) {
	_, err := Parse("AGENTS.md", []byte("Foundry guides: bad\nFoundry guides: also-bad\n"))
	message, owned := safeerror.OwnedMessage(err)
	if !owned || !strings.Contains(message, "AGENTS.md:1:") || !strings.Contains(message, "AGENTS.md:2:") {
		t.Fatalf("lost captured diagnostics: %q", message)
	}
	var location Problem
	if !errors.As(err, &location) || location.File != "AGENTS.md" || location.Line != 1 {
		t.Fatalf("lost typed location: %+v, %v", location, err)
	}
	one := problem("AGENTS.md", 3, "invalid reference")
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
			refs, err := Parse("AGENTS.md", []byte(tc.text))
			if (err != nil) != tc.bad || len(refs) != tc.count {
				t.Fatalf("refs=%v err=%v", refs, err)
			}
			if err != nil && !strings.Contains(err.Error(), "AGENTS.md:1:") {
				t.Fatal(err)
			}
		})
	}
	refs, err := Parse("skills/recipe.md", []byte("intro\nFoundry guides: `orm#entry`\nFoundry guides: `http`"))
	if err != nil || refs[0] != (Reference{"skills/recipe.md", 2, "orm", "entry"}) || refs[1].Line != 3 {
		t.Fatalf("%v %v", refs, err)
	}
}

func TestExplicitAnchorIndexAndNavigation(t *testing.T) {
	data := []byte("# A heading is not an ID\n<a id=\"agent-entry\"></a>\n## Agent entry\n```html\n<a id=\"agent-entry\"></a>\n<a id='BAD'></a>\n```\n<a id=\"recipe-2\"></a>\n## Recipe\n[entry](#agent-entry)\nAn ID uses `<a id=\"example\"></a>` syntax.")
	index, err := IndexAnchors("orm.md", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"agent-entry", "recipe-2"} {
		if err := index.CheckFragment("orm.md", 10, fragment); err != nil {
			t.Fatal(err)
		}
	}
	for _, fragment := range []string{"a-heading-is-not-an-id", "missing", "#agent-entry", "Agent-Entry", "%61gent-entry", ""} {
		if err := index.CheckFragment("orm.md", 10, fragment); err == nil || !strings.Contains(err.Error(), "orm.md:10:") {
			t.Fatalf("fragment %q: %v", fragment, err)
		}
	}
	for _, anchor := range []string{`<a id="UPPER"></a>`, `<a id="-edge"></a>`, `<a id="edge-"></a>`, `<a id=""></a>`, `<a id='entry'></a>`, `<a id="entry" />`, `<a id="entry"></a> trailing`, `<a id="a/b"></a>`} {
		if _, err := IndexAnchors("guide.md", []byte("intro\n"+anchor)); err == nil || !strings.Contains(err.Error(), "guide.md:2:") {
			t.Fatalf("%s: %v", anchor, err)
		}
	}
	_, err = IndexAnchors("guide.md", []byte("<a id=\"entry\"></a>\n# Title\n<a id=\"entry\"></a>"))
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
		{"orm", "heading", "absent"}, {"duplicate", "", "duplicate explicit anchor"}, {"../orm", "", "invalid guide"},
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
	refs, err := Parse("AGENTS.md", []byte("Foundry guides: `orm#entry`"))
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
