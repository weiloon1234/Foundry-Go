package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func TestDeployCommand(t *testing.T) {
	for _, mode := range []string{"success", "version-mismatch", "development", "no-tool", "missing-python", "go-failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			frameworkDir := t.TempDir()
			entry := filepath.Join(frameworkDir, frameworkinfo.DeployEntry)
			if mode != "no-tool" {
				if err := os.MkdirAll(filepath.Dir(entry), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(entry, nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			metadata, err := json.Marshal(struct{ Path, Version, Dir string }{frameworkinfo.ModulePath, "v0.5.3", frameworkDir})
			if err != nil {
				t.Fatal(err)
			}
			goBinary := filepath.Join(t.TempDir(), "go")
			script := "#!/bin/sh\nprintf '%s\\n' '" + string(metadata) + "'\n"
			if mode == "go-failure" {
				script = "#!/bin/sh\nprintf '%s\\n' 'private tool output'\nexit 1\n"
			}
			if err := os.WriteFile(goBinary, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			python := filepath.Join(t.TempDir(), "python")
			if mode != "missing-python" {
				if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			build := frameworkinfo.Build{Version: "v0.5.3"}
			if mode == "version-mismatch" {
				build.Version = "v0.6.0"
			} else if mode == "development" {
				build.Version = ""
			}
			path, argv, err := deployCommand(t.Context(), deployOptions{
				dir: dir, goBinary: goBinary, python: python, build: build,
				arguments: []string{"deploy", "--environment", "production"},
			})
			var wantErrors []string
			switch mode {
			case "version-mismatch":
				wantErrors = []string{"v0.6.0", "v0.5.3"}
			case "no-tool":
				wantErrors = []string{"no deploy tool"}
			case "missing-python":
				wantErrors = []string{"Python was not found"}
			case "go-failure":
				wantErrors = []string{"go mod download " + frameworkinfo.ModulePath}
			}
			if len(wantErrors) > 0 {
				if err == nil {
					t.Fatal("expected deploy prerequisite error")
				}
				for _, want := range wantErrors {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error %q omitted %q", err, want)
					}
				}
				if strings.Contains(err.Error(), "private tool output") {
					t.Fatal("deploy echoed private tool output")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{python, "-B", entry, "--app-root", absDir, "deploy", "--environment", "production"}
			if path != python || !reflect.DeepEqual(argv, want) {
				t.Fatalf("deploy command = %q %q, want %q %q", path, argv, python, want)
			}
		})
	}
}
