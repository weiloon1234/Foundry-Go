package retirement

import (
	"strings"
	"testing"
)

func TestBareNameReportDoesNotNameAnOwnerTheDocumentNeverWrote(t *testing.T) {
	err := CheckDocument("guide.md", []byte("# Guide\n\nThe `Compile` span stands alone.\n"))
	want := "guide.md:3: `Compile` matches the retired `rows.Compile`; write the qualified name you mean (for example `pkg.Compile`), or use `rows.CompileRead`."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
	if strings.Contains(err.Error(), "rows.Compile is retired") {
		t.Fatalf("bare name attributed to a package the document did not write: %v", err)
	}
	// A qualified spelling is the document's own claim and keeps the direct wording.
	err = CheckDocument("guide.md", []byte("Use `rows.Compile` here.\n"))
	if err == nil || !strings.Contains(err.Error(), "rows.Compile is retired; use rows.CompileRead.") {
		t.Fatalf("qualified spelling: %v", err)
	}
}

func TestBareNameReportListsEveryRetiredMember(t *testing.T) {
	rules := documentRules{replacements: map[string]string{
		"rows.Count":     "rows.Read(query).Count(ctx, executor)",
		"query.Count":    "",
		"orm.Page.Count": "orm.Page.Total",
		"rows.Each":      "rows.EachRead",
		"rows.Gone":      "",
	}}
	message := rules.retirementMessage("Count")
	want := "`Count` matches the retired `orm.Page.Count`, `query.Count`, `rows.Count`; write the qualified name you mean (for example `pkg.Count`), or use `orm.Page.Total` for `orm.Page.Count`, `rows.Read(query).Count(ctx, executor)` for `rows.Count`."
	if message != want {
		t.Fatalf("got  %q\nwant %q", message, want)
	}
	if got := rules.retirementMessage("Gone"); got != "`Gone` matches the retired `rows.Gone`; write the qualified name you mean (for example `pkg.Gone`)." {
		t.Fatalf("no replacement: %q", got)
	}
	if got := rules.retirementMessage("Unknown"); !strings.Contains(got, "no replacement is recorded") || strings.Contains(got, "matches the retired") {
		t.Fatalf("unmatched name: %q", got)
	}
}

func TestCommentDoesNotHideRetiredTextAfterIt(t *testing.T) {
	for name, text := range map[string]string{
		"after a comment":            "<!-- c --> rows.Count(q)\n",
		"after a multi-line comment": "# T\n\n<!-- a\nb --> rows.Count(q)\n",
		"before a comment":           "rows.Count(q) <!-- c -->\n",
		"between comments":           "<!-- a --> rows.Count(q) <!-- b -->\n",
		"after a comment block":      "<!-- c -->\n\n<!-- d --> rows.Count(q)\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := CheckDocument("guide.md", []byte(text))
			if err == nil || !strings.Contains(err.Error(), "rows.Count is retired") {
				t.Fatalf("retired text beside a comment was not checked: %v", err)
			}
		})
	}
	line := CheckDocument("guide.md", []byte("<!-- a\nb --> rows.Count(q)\n"))
	if line == nil || !strings.Contains(line.Error(), "guide.md:2:") {
		t.Fatalf("diagnostic line: %v", line)
	}
	for _, text := range []string{"<!-- rows.Count(q) -->\n", "<!-- a\nrows.Count(q)\nb -->\n", "<!-- a --> <!-- rows.Count(q) -->\n"} {
		if err := CheckDocument("guide.md", []byte(text)); err != nil {
			t.Errorf("comment contents are not teaching, got %v for %q", err, text)
		}
	}
}

func TestDocumentShapeRefusalsReachBothCheckers(t *testing.T) {
	for name, text := range map[string]string{
		"CR-only":  "# T\r\r```go\rrows.Count(q)\r```\r",
		"nesting":  strings.Repeat("> ", 17) + "x\n",
		"too long": strings.Repeat("a", 20000) + "\n",
	} {
		for label, check := range map[string]func(string, []byte) error{"framework": CheckDocument, "application": CheckApplicationDocument} {
			if err := check("AGENTS.md", []byte(text)); err == nil || !strings.Contains(err.Error(), "AGENTS.md:") {
				t.Errorf("%s/%s: shape violation not refused with a location: %v", label, name, err)
			}
		}
	}
}

func TestUnclosedCommentIsRefusedNotSkipped(t *testing.T) {
	err := CheckDocument("guide.md", []byte("# T\n\n<!-- abc\nrows.Count(q)\n"))
	if err == nil || !strings.Contains(err.Error(), `guide.md:3: HTML comment is not closed; add "-->"`) {
		t.Fatalf("unclosed comment hid the rest of the document: %v", err)
	}
}
