package docrefs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSkillIndexCountsOnlySkillFilesAndAnchors(t *testing.T) {
	skills := SkillIndex([]Reference{
		{File: ".claude/skills/endpoint/SKILL.md", Line: 3, Guide: "flat-routes", Anchor: "staged-routes"},
		{File: ".claude/skills/review/SKILL.md", Line: 3, Guide: "flat-routes", Anchor: "staged-routes"},
		{File: ".claude/skills/endpoint/SKILL.md", Line: 4, Guide: "flat-routes"},
		{File: ".claude/skills/endpoint/references/api.md", Line: 1, Guide: "model-declarations", Anchor: "recipe-declare-table"},
		{File: "AGENTS.md", Line: 1, Guide: "model-declarations", Anchor: "recipe-declare-table"},
		{File: ".claude/skills/SKILL.md", Line: 1, Guide: "model-declarations", Anchor: "recipe-declare-table"},
	})
	want := map[string][]string{"flat-routes#staged-routes": {"endpoint", "review"}}
	if !reflect.DeepEqual(map[string][]string(skills), want) {
		t.Fatalf("index = %v", skills)
	}
}

func TestScanSkillsReadsEachSkillOnceAndSkipsWhatItCannotRead(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/skills/endpoint/SKILL.md", "Foundry guides: `flat-routes#staged-routes`\n")
	write(".claude/skills/models/SKILL.md", "Foundry guides: `model-declarations#recipe-declare-table`, `flat-routes#staged-routes`\n")
	write(".claude/skills/broken/SKILL.md", "Foundry guides: not a declaration\n")
	write(".claude/skills/empty/notes.md", "no skill file\n")
	write(".claude/skills/secret/SKILL.md", "Foundry guides: `flat-routes#staged-routes`\n")
	if err := os.Symlink(filepath.Join(root, ".claude/skills/secret/SKILL.md"), filepath.Join(root, ".claude/skills/linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("secret", filepath.Join(root, ".claude/skills/alias")); err != nil {
		t.Fatal(err)
	}
	got := ScanSkills(root)
	want := map[string][]string{
		"flat-routes#staged-routes":               {"endpoint", "models", "secret"},
		"model-declarations#recipe-declare-table": {"models"},
	}
	if !reflect.DeepEqual(map[string][]string(got), want) {
		t.Fatalf("skills = %v", got)
	}
	if ScanSkills(t.TempDir()) != nil {
		t.Fatal("an application without skills named some")
	}
}
