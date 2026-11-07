package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func putDocumentation(t *testing.T, root, name, text string) {
	t.Helper()
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func documentationFixture(t *testing.T, mode string) (app, framework, goBinary string, build frameworkinfo.Build) {
	t.Helper()
	app, framework = t.TempDir(), t.TempDir()
	putDocumentation(t, app, "go.mod", "module example.test/app\nrequire "+frameworkinfo.ModulePath+" v0.11.0\n")
	putDocumentation(t, framework, "go.mod", "module "+frameworkinfo.ModulePath+"\n")
	putDocumentation(t, framework, "docs/guides/orm.md", "<a id=\"agent-entry\"></a>\n# Models\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#agent-entry`\n")
	metadata := struct {
		Path, Version, Dir string
		Main               bool
		Replace            *struct{ Version string } `json:",omitempty"`
	}{Path: frameworkinfo.ModulePath, Version: "v0.11.0", Dir: framework}
	build = frameworkinfo.Build{Version: "v0.11.0"}
	switch mode {
	case "mismatch":
		build.Version = "v0.12.0"
	case "unversioned-tool":
		build.Version = ""
	case "unversioned-selection":
		metadata.Version = ""
	case "main":
		metadata.Main = true
	case "local":
		metadata.Replace = &struct{ Version string }{}
	case "versioned-replacement":
		metadata.Version = "v0.10.0"
		metadata.Replace = &struct{ Version string }{Version: "v0.11.0"}
	case "missing-cache":
		metadata.Dir = ""
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	// Use the existing doctor executable seam. The child fails if selection could
	// download, mutate go.mod, consult a workspace or execute anything but metadata.
	script := `#!/bin/sh
test "$GOWORK" = off && test "$GOPROXY" = off && test "$GOSUMDB" = off &&
test "$GOTOOLCHAIN" = local && test "$GOFLAGS" = -mod=readonly &&
test "$GOENV" = off && test "$GOVCS" = '*:off' || exit 91
test "$1 $2 $3 $4" = 'list -m -json github.com/weiloon1234/Foundry-Go' || exit 92
printf '%s\n' ` + "'" + strings.ReplaceAll(string(data), "'", "'\"'\"'") + "'\n"
	goBinary = filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(goBinary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return
}

func documentationSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%v %d", info.Mode(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %x", sha256.Sum256(data))
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			value += target
		}
		snapshot[rel] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestDocsSelectedVersionAndOfflinePolicy(t *testing.T) {
	for _, mode := range []string{"match", "versioned-replacement", "mismatch", "unversioned-tool", "unversioned-selection", "main", "local", "missing-cache"} {
		t.Run(mode, func(t *testing.T) {
			app, framework, goBinary, build := documentationFixture(t, mode)
			beforeApp, beforeFramework := documentationSnapshot(t, app), documentationSnapshot(t, framework)
			err := checkDocs(t.Context(), app, goBinary, build)
			want := ""
			switch mode {
			case "mismatch":
				want = "module-pinned"
			case "unversioned-tool", "unversioned-selection", "main", "local":
				want = "unverifiable"
			case "missing-cache":
				want = "go mod download"
			}
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeApp, documentationSnapshot(t, app)) || !reflect.DeepEqual(beforeFramework, documentationSnapshot(t, framework)) {
				t.Fatal("documentation check mutated app or selected module")
			}
		})
	}
}

func TestDocsActiveModuleInstructions(t *testing.T) {
	app, framework, goBinary, build := documentationFixture(t, "match")
	bad := "Foundry guides: `missing#anchor`\nUse `http.Endpoint.WithScopes`.\n"
	for _, dir := range []string{"node_modules", "vendor", "build", "dist", "target", ".cache", ".git", ".agents/skills", "testdata", "history/2026-11-05", ".claude/skills/workflow/history/20261105"} {
		putDocumentation(t, app, dir+"/AGENTS.md", bad)
		putDocumentation(t, app, dir+"/references.md", bad)
	}
	putDocumentation(t, app, ".claude/skills/workflow/notes-2026-11-05.md", bad)
	putDocumentation(t, app, "other-module/go.mod", "module example.test/other\n")
	putDocumentation(t, app, "other-module/AGENTS.md", bad)
	putDocumentation(t, app, "README.md", bad)
	putDocumentation(t, app, "web/AGENTS.md", "Foundry guides: `orm`\n")
	putDocumentation(t, app, ".claude/skills/workflow/references/recipe.md", "<!-- app-owned:begin policy -->\nFoundry guides: `orm#agent-entry`\n<!-- app-owned:end policy -->\n")
	if err := checkDocs(t.Context(), filepath.Join(app, "web"), goBinary, build); err != nil {
		t.Fatal(err)
	}
	// Check nested app-owned reference files as well as SKILL.md itself.
	putDocumentation(t, app, ".claude/skills/workflow/references/recipe.md", "<!-- app-owned:begin policy -->\n"+bad+"<!-- app-owned:end policy -->\n")
	beforeApp, beforeFramework := documentationSnapshot(t, app), documentationSnapshot(t, framework)
	err := checkDocs(t.Context(), app, goBinary, build)
	if err == nil {
		t.Fatal("accepted app-owned stale teaching")
	}
	for _, want := range []string{"recipe.md:2:", "recipe.md:3:", "missing", "retired API"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
	if !reflect.DeepEqual(beforeApp, documentationSnapshot(t, app)) || !reflect.DeepEqual(beforeFramework, documentationSnapshot(t, framework)) {
		t.Fatal("failed check mutated files")
	}
}

func TestDocsRejectsWrongSelectedGuideAndToolBeforeScanning(t *testing.T) {
	app, framework, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, app, "docs/guides/orm.md", "<a id=\"only-in-app\"></a>\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#only-in-app`\n")
	if err := checkDocs(t.Context(), app, goBinary, build); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatal(err)
	}
	putDocumentation(t, framework, "docs/guides/orm.md", "# Old pin without anchors\n")
	putDocumentation(t, app, "AGENTS.md", "Foundry guides: `orm#agent-entry`\n")
	if err := checkDocs(t.Context(), app, goBinary, build); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatal(err)
	}
	build.Version = "v0.12.0"
	if err := checkDocs(t.Context(), app, goBinary, build); err == nil || !strings.Contains(err.Error(), "v0.12.0") || strings.Contains(err.Error(), "absent") {
		t.Fatal(err)
	}
}

func TestDocsSkillsMirrorIsNotFollowed(t *testing.T) {
	app, _, goBinary, build := documentationFixture(t, "match")
	putDocumentation(t, app, ".claude/skills/workflow/SKILL.md", "Foundry guides: `orm`\n")
	if err := os.Mkdir(filepath.Join(app, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.claude/skills", filepath.Join(app, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	if err := checkDocs(t.Context(), app, goBinary, build); err != nil {
		t.Fatal(err)
	}
}

func TestDocsCommandUsage(t *testing.T) {
	for _, args := range [][]string{{"docs"}, {"docs", "write"}, {"docs", "check", "--write"}, {"docs", "check", "--version", "v0.11.0"}, {"docs", "check", "unexpected"}} {
		if err := run(t.Context(), args, io.Discard, io.Discard); cli.Status(err) != cli.InvalidUsage {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if err := checkDocs(t.Context(), t.TempDir(), "go", frameworkinfo.Build{}); err == nil || !strings.Contains(err.Error(), "Go module") {
		t.Fatal(err)
	}
}
