package markdown

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuin/goldmark/parser"
)

func TestShapeRefusalsAreLocatedAndPrecedeParsing(t *testing.T) {
	nested := func(marker string, n int) string { return strings.Repeat(marker, n) + "x\n" }
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"quote markers", "# T\n\n" + nested("> ", MaxContainerMarkers+1), "guide.md:3: line opens with more than 16 quote or list markers"},
		{"packed quote markers", "a\n" + nested(">", MaxContainerMarkers+1), "guide.md:2: line opens with more than 16 quote or list markers"},
		{"bullets", nested("- ", MaxContainerMarkers+1), "guide.md:1: line opens with more than 16"},
		{"ordered items", nested("1. ", MaxContainerMarkers+1), "guide.md:1: line opens with more than 16"},
		{"mixed", strings.Repeat("> - ", 9) + "x\n", "guide.md:1: line opens with more than 16"},
		{"long line", "ok\n" + strings.Repeat("a", MaxLineBytes+1) + "\n", "guide.md:2: line exceeds maximum 16384 bytes"},
		{"CR-only endings", "# T\r\r```go\rx := 1\r```\r", "guide.md:1: document uses CR-only line endings"},
		{"lone CR mid-line", "a\nb\rc\n", "guide.md:2: document uses CR-only line endings"},
		{"trailing lone CR", "a\r", "guide.md:1: document uses CR-only line endings"},
		{"oversize", strings.Repeat("a\n", MaxDocumentBytes/2+1), "guide.md:0: document exceeds maximum 524288 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := time.Now()
			_, err := Read("guide.md", []byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("refusal took %v; it must not parse", elapsed)
			}
		})
	}
}

func TestShapeAcceptsOrdinaryNesting(t *testing.T) {
	for name, input := range map[string]string{
		"limit quote markers": strings.Repeat("> ", MaxContainerMarkers) + "x\n",
		"CRLF":                "# T\r\n\r\n```go\r\nx := 1\r\n```\r\n",
		"hyphen text":         "-not a bullet - - - - - - - - - - - - - - - - - - -\n",
		"digits in prose":     "1234567890 > > > > > > > > > > > > > > > > > > > >\n",
		"limit line":          strings.Repeat("a", MaxLineBytes) + "\n",
	} {
		if _, err := Read("guide.md", []byte(input)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDocumentLimitIsOneRulePerPath(t *testing.T) {
	big := []byte(strings.Repeat("a\n", MaxDocumentBytes/2+10))
	for _, name := range []string{"docs/guides/a.md", ".claude/skills/x/SKILL.md", "AGENTS.md", "blueprint/00-master.md", "docs/evidenceish/a.md", "sub/CHANGELOG.md"} {
		if DocumentLimit(name) != MaxDocumentBytes {
			t.Errorf("%s limit = %d", name, DocumentLimit(name))
		}
		if _, err := Read(name, big); err == nil {
			t.Errorf("%s: oversize document accepted", name)
		}
	}
	for _, name := range []string{"docs/evidence/a.md", "CHANGELOG.md", "docs/./evidence/b.md"} {
		if DocumentLimit(name) != MaxRecordBytes {
			t.Errorf("%s limit = %d", name, DocumentLimit(name))
		}
		if _, err := Read(name, big); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Read("CHANGELOG.md", []byte(strings.Repeat("a\n", MaxRecordBytes/2+10))); err == nil || !strings.Contains(err.Error(), "exceeds maximum 1572864 bytes") {
		t.Fatalf("record limit not enforced: %v", err)
	}
}

// The worst inputs the limits still admit: each is at the size limit, within
// the line and nesting limits, and is costly for the parser. The budget is far
// above the measured time so a loaded machine does not fail it, and far below
// the minutes the same inputs took before the limits.
func TestWorstAdmittedInputsParseWithinBudget(t *testing.T) {
	const budget = 15 * time.Second
	size := MaxDocumentBytes - 64
	repeat := func(unit string) []byte { return []byte(strings.Repeat(unit, size/len(unit))) }
	wide := func(unit string) []byte { return repeat(strings.Repeat(unit, MaxLineBytes/len(unit)) + "\n") }
	inputs := map[string][]byte{
		"link openers across lines":  repeat("[a](\n"),
		"link openers on long lines": wide("[a]("),
		"reference definitions":      repeat("[a]: /u\n"),
		"deepest quotes":             repeat(strings.Repeat("> ", MaxContainerMarkers) + "x\n"),
		"deepest bullets":            repeat(strings.Repeat("- ", MaxContainerMarkers) + "x\n"),
		"unmatched brackets":         wide("["),
		"emphasis openers":           wide("*a "),
		"inline code openers":        wide("` "),
		"table rows":                 []byte("| a | b |\n|---|---|\n" + strings.Repeat("| 1 | 2 |\n", size/10)),
	}
	for name, data := range inputs {
		start := time.Now()
		document, err := Read("guide.md", data)
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("%s: refused: %v", name, err)
			continue
		}
		t.Logf("%-28s %7d bytes %5d blocks %v", name, len(data), len(document.Blocks), elapsed.Round(time.Millisecond))
		if elapsed > budget {
			t.Errorf("%s: parse took %v, budget %v", name, elapsed, budget)
		}
	}
}

// Read replaces goldmark's fenced-code parser by its registration priority. If
// a goldmark upgrade renumbers or splits that registration, the replacement
// would silently not be installed and every fence offset would be lost.
func TestFenceParserReplacementTargetsGoldmarkFenceRegistration(t *testing.T) {
	fenceType := reflect.TypeOf(parser.NewFencedCodeBlockParser())
	var matching int
	for _, registered := range parser.DefaultBlockParsers() {
		if reflect.TypeOf(registered.Value) == fenceType {
			matching++
			if registered.Priority != fencePriority {
				t.Fatalf("goldmark registers its fenced-code parser at priority %d; update fencePriority", registered.Priority)
			}
		} else if registered.Priority == fencePriority {
			t.Fatalf("goldmark registers %T at fence priority %d; the fence replacement would clobber it", registered.Value, fencePriority)
		}
	}
	if matching != 1 {
		t.Fatalf("goldmark registers %d fenced-code parsers, want 1", matching)
	}
	document, err := Read("guide.md", []byte("```go\nx := 1\n```\n"))
	if err != nil || len(document.Blocks) != 1 || document.Blocks[0].Kind != Fence || !document.Blocks[0].Closed || document.Blocks[0].BodyEnd <= document.Blocks[0].BodyStart {
		t.Fatalf("fence adapter not installed: %v %+v", err, document.Blocks)
	}
}

func TestContainerContextIsCarriedDownTheWalk(t *testing.T) {
	document, err := Read("guide.md", []byte("plain\n\n> quoted\n>\n> - listed in quote\n\n- item\n\n  after\n\nlast\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]bool{}
	for _, block := range document.Blocks {
		if block.Kind == Prose {
			got[strings.TrimSpace(block.Text)] = [2]bool{block.Quoted, block.Listed}
		}
	}
	want := map[string][2]bool{"plain": {false, false}, "quoted": {true, false}, "listed in quote": {true, true}, "item": {false, true}, "after": {false, true}, "last": {false, false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("context = %v, want %v", got, want)
	}
}

func TestTableRowsExposeCells(t *testing.T) {
	document, err := Read("guide.md", []byte("```\n| not | a table |\n```\n\n| N | Name | Status |\n| --- | --- | --- |\n| 01 | [Core](c.md) | **Complete** - done |\n| 02 | `x\\|y` |  |\n"))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for _, block := range document.Blocks {
		if block.Kind == TableRow {
			rows = append(rows, block.Cells)
		}
	}
	want := [][]string{{"N", "Name", "Status"}, {"01", "Core", "Complete - done"}, {"02", "x|y", ""}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
}

func TestCacheSharesOneParsePerDocument(t *testing.T) {
	cache := NewCache()
	data := []byte("# T\n\n```go\nx := 1\n```\n")
	first, err := cache.Read("a.md", data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.Read("a.md", append([]byte(nil), data...))
	if err != nil || len(first.Blocks) == 0 || again.Blocks[0].syntax != first.Blocks[0].syntax {
		t.Fatalf("second read reparsed: %v", err)
	}
	changed, err := cache.Read("a.md", []byte("# Other\n"))
	if err != nil || changed.Blocks[0].syntax == first.Blocks[0].syntax || changed.Blocks[0].Text != "Other" {
		t.Fatalf("changed content served from cache: %v %+v", err, changed.Blocks)
	}
	unchanged, _ := cache.Read("a.md", data)
	if unchanged.Blocks[0].syntax != first.Blocks[0].syntax {
		t.Fatal("original entry lost after a different read under the same name")
	}
	if _, err := cache.Read("big.md", []byte(strings.Repeat("a", MaxDocumentBytes+1))); err == nil {
		t.Fatal("cache accepted an oversize document")
	}
	var none *Cache
	if _, err := none.Read("a.md", data); err != nil {
		t.Fatalf("nil cache: %v", err)
	}
}

func TestCacheRetentionIsBounded(t *testing.T) {
	cache := NewCache()
	for i := 0; i < MaxDocuments+10; i++ {
		if _, err := cache.Read("d"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+strings.Repeat("y", i/26)+".md", []byte("t\n")); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) > MaxDocuments {
		t.Fatalf("cache retained %d documents, maximum %d", len(cache.entries), MaxDocuments)
	}
}

func TestBudgetBoundsDocumentsAndBytes(t *testing.T) {
	var documents Budget
	for i := 0; i < MaxDocuments; i++ {
		if err := documents.Take("a.md", 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := documents.Take("last.md", 1); err == nil || !strings.Contains(err.Error(), "last.md:0: too many documents; maximum is 4096.") {
		t.Fatalf("document count: %v", err)
	}
	var bytes Budget
	if err := bytes.Take("a.md", MaxRunBytes); err != nil {
		t.Fatal(err)
	}
	if err := bytes.Take("b.md", 1); err == nil || !strings.Contains(err.Error(), "b.md:0: documents exceed maximum") {
		t.Fatalf("byte total: %v", err)
	}
}

func TestMixedCommentBlockKeepsItsTail(t *testing.T) {
	for input, kind := range map[string]Kind{
		"<!-- c -->\n":                  Comment,
		"<!-- a\nb -->\n":               Comment,
		"<!-- a --> <!-- b -->\n":       Comment,
		"<!-- c --><pre>x</pre>\n":      HTML,
		"<!-- c --> tail text\n":        HTML,
		"<!-- a\nb --><pre>x</pre>\n":   HTML,
		"<!-- unterminated\nstill in\n": Comment,
	} {
		document, err := Read("guide.md", []byte(input))
		if err != nil || len(document.Blocks) != 1 || document.Blocks[0].Kind != kind {
			t.Errorf("%q: kind %v, want %v (%v)", input, document.Blocks, kind, err)
		}
	}
}

func TestSiblingIndexesResolveWithinOneContainer(t *testing.T) {
	adjacent, err := Read("guide.md", []byte("<!-- m -->\n```go\nx\n```\n"))
	if err != nil || len(adjacent.Blocks) != 2 || adjacent.Blocks[0].Next != 1 || adjacent.Blocks[1].Previous != 0 || adjacent.Blocks[0].Previous != -1 || adjacent.Blocks[1].Next != -1 {
		t.Fatalf("adjacent siblings: %v %+v", err, adjacent.Blocks)
	}
	// A fence in another container, or behind a list, is not the marker's sibling.
	for name, input := range map[string]string{
		"list between":  "<!-- m -->\n\n- ```go\n  x\n  ```\n",
		"quote between": "<!-- m -->\n\n> ```go\n> x\n> ```\n",
	} {
		document, err := Read("guide.md", []byte(input))
		if err != nil || document.Blocks[0].Kind != Comment || document.Blocks[0].Next != -1 {
			t.Errorf("%s: marker sibling = %d (%v)", name, document.Blocks[0].Next, err)
		}
	}
}
