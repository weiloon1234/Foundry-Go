package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentCoordinatesAndUnsavedInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "space #.go")
	source := "package models\r\n// 🦀probe\r\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := prepare(Options{Workspace: dir, File: path, Line: 2, Column: 8, Insert: "X\n中"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.position != (Position{2, 1}) || !strings.Contains(doc.text, "🦀X\n中probe") {
		t.Fatalf("document = %+v", doc)
	}
	if !strings.Contains(doc.uri, "space%20%23.go") {
		t.Fatalf("URI = %s", doc.uri)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != source {
		t.Fatalf("source changed: %q %v", data, err)
	}
	if got := lspPosition("first\r\n// 🦀X"); got != (Position{1, 6}) {
		t.Fatalf("UTF-16 position = %+v", got)
	}
}

func TestByteCoordinates(t *testing.T) {
	source := "é\r\n🦀x\n"
	for _, tc := range []struct{ line, column, offset int }{{1, 1, 0}, {1, 3, 2}, {2, 1, 4}, {2, 5, 8}, {2, 6, 9}, {3, 1, 10}} {
		got, err := byteOffset(source, tc.line, tc.column)
		if err != nil || got != tc.offset {
			t.Fatalf("%+v = %d, %v", tc, got, err)
		}
	}
	for _, pos := range [][2]int{{0, 1}, {1, 0}, {1, 2}, {1, 4}, {2, 2}, {2, 7}, {4, 1}} {
		if _, err := byteOffset(source, pos[0], pos[1]); err == nil {
			t.Fatalf("accepted position %v", pos)
		}
	}
}

func TestDocumentRefusesOutsideWorkspaceAndInvalidInput(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{outside, link} {
		if _, err := prepare(Options{Workspace: dir, File: file, Line: 1, Column: 1}); err == nil {
			t.Fatal("accepted outside source")
		}
	}
	path := filepath.Join(dir, "invalid.go")
	for _, data := range [][]byte{{0xff}, []byte(strings.Repeat(" ", maxDocumentBytes+1))} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := prepare(Options{Workspace: dir, File: path, Line: 1, Column: 1}); err == nil {
			t.Fatal("accepted invalid source")
		}
	}
}
