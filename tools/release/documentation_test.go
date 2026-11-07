package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This package acceptance case uses the real candidate and an independent
// consumer. All setup is file-proxy-only; dependencies must already be cached.
// The test owns its module cache. It never installs into the developer's cache.
func TestDocumentationPackagedOfflineConsumer(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "candidate")
	const version = "v0.0.0-docrefs-candidate"
	if err := prepare(options{root: root, out: out, version: version}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report manifest
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	framework := report.Artifacts[0]
	archive, err := zip.OpenReader(filepath.Join(out, filepath.FromSlash(framework.Zip)))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	const guide = "docs/guides/developer-tooling-and-testing.md"
	const anchor = "pinned-documentation-checks"
	required := map[string]bool{
		guide: false, "internal/docrefs/references.go": false, "internal/docrefs/anchors.go": false,
		"internal/retirement/denied.json": false, "internal/retirement/document_rules.json": false,
		"internal/retirement/document_owners.go": false, "cmd/foundry/docs.go": false,
	}
	var guideBytes []byte
	for _, file := range archive.File {
		name := strings.TrimPrefix(file.Name, framework.Path+"@"+version+"/")
		if strings.HasPrefix(name, "tests/fixtures/") {
			t.Fatal("candidate includes nested fixtures")
		}
		if _, needed := required[name]; !needed {
			continue
		}
		required[name] = true
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if name == guide {
			guideBytes = data
		}
	}
	for name, present := range required {
		if !present {
			t.Fatalf("candidate omitted checker resource %s", name)
		}
	}
	if !bytes.Contains(guideBytes, []byte(`<a id="`+anchor+`"></a>`)) {
		t.Fatal("packaged guide lost stable anchor")
	}

	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("install the project Go toolchain before package acceptance")
	}
	environment := documentationOfflineEnv(os.Environ())
	warmCache := strings.TrimSpace(documentationCommand(t, root, environment, goBinary, "env", "GOMODCACHE"))
	cache := t.TempDir()
	t.Cleanup(func() {
		// Go extracts module directories read-only. Restore directory permissions
		// only in this test's cache so TempDir can remove it after acceptance.
		if err := filepath.WalkDir(cache, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(name, 0700)
			}
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	app := t.TempDir()
	put(t, app, "go.mod", "module example.test/docs-consumer\n\ngo "+report.Go+"\n\nrequire "+framework.Path+" "+version+"\n\ntool "+framework.Path+"/cmd/foundry\n")
	proxyURL := func(path string) string { return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String() }
	// Explicit setup reads only the new private candidate and cached dependency ZIPs.
	setup := append(append([]string(nil), environment...), "GOMODCACHE="+cache, "GOPROXY="+proxyURL(filepath.Join(out, "proxy"))+","+proxyURL(filepath.Join(warmCache, "cache", "download")), "GOFLAGS=-mod=mod")
	// Resolve the pinned tool as an application would, recording its module and
	// dependency checksums before go tool loads it and before going read-only.
	documentationCommand(t, app, setup, goBinary, "get", "-tool", framework.Path+"/cmd/foundry@"+version)
	sums, err := os.ReadFile(filepath.Join(app, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+string(sums), "\n"+framework.Path+" "+version+" "+framework.Sum+"\n") ||
		!strings.Contains("\n"+string(sums), "\n"+framework.Path+" "+version+"/go.mod h1:") {
		t.Fatal("consumer go.sum does not cover the packaged candidate module and go.mod")
	}
	documentationCommand(t, app, setup, goBinary, "tool", "foundry", "docs", "--help")
	offline := append(append([]string(nil), environment...), "GOMODCACHE="+cache)
	selected := strings.TrimSpace(documentationCommand(t, app, offline, goBinary, "list", "-m", "-f", "{{.Dir}}", framework.Path))
	if !strings.HasPrefix(selected, cache+string(filepath.Separator)) {
		t.Fatalf("consumer resolved outside private module cache: %s", selected)
	}
	cachedGuide, err := os.ReadFile(filepath.Join(selected, filepath.FromSlash(guide)))
	if err != nil || !bytes.Equal(cachedGuide, guideBytes) {
		t.Fatal("offline guide differs from packaged guide", err)
	}
	module, err := readModule(app)
	if err != nil || len(module.Replace) != 0 {
		t.Fatal("consumer acquired a local replacement", err)
	}
	put(t, app, "AGENTS.md", "Foundry guides: `developer-tooling-and-testing#"+anchor+"`\n")
	put(t, app, ".claude/skills/example/references.md", "Foundry guides: `developer-tooling-and-testing`\n")
	beforeApp, beforeCache := documentationTree(t, app), documentationTree(t, cache)
	output := documentationCommand(t, app, offline, goBinary, "tool", "foundry", "docs", "check", "--dir", ".")
	if !strings.Contains(output, "Pinned documentation references") {
		t.Fatal(output)
	}
	if !reflect.DeepEqual(beforeApp, documentationTree(t, app)) || !reflect.DeepEqual(beforeCache, documentationTree(t, cache)) {
		t.Fatal("checker wrote to app or module cache")
	}
	put(t, app, "AGENTS.md", "Foundry guides: `developer-tooling-and-testing#"+anchor+"`\nUse `http.Endpoint.WithBodyValidation`.\n")
	beforeApp = documentationTree(t, app)
	output, err = documentationResult(t, app, offline, goBinary, "tool", "foundry", "docs", "check", "--dir", ".")
	if err == nil || !strings.Contains(output, "AGENTS.md:2:") || !strings.Contains(output, "retired API") {
		t.Fatalf("packaged checker did not apply its embedded retirement rules: %v\n%s", err, output)
	}
	if !reflect.DeepEqual(beforeApp, documentationTree(t, app)) || !reflect.DeepEqual(beforeCache, documentationTree(t, cache)) {
		t.Fatal("failed checker wrote to app or module cache")
	}
}

func documentationOfflineEnv(source []string) []string {
	var result []string
	for _, item := range source {
		name, _, _ := strings.Cut(item, "=")
		switch name {
		case "GOWORK", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "GONOSUMDB", "GOPRIVATE", "GONOPROXY", "GOVCS", "GOFLAGS", "GOENV":
			continue
		}
		result = append(result, item)
	}
	return append(result, "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GONOSUMDB=*", "GOPRIVATE=", "GONOPROXY=none", "GOVCS=*:off", "GOFLAGS=-mod=readonly", "GOENV=off")
}

type documentationOutput struct{ data bytes.Buffer }

func (b *documentationOutput) String() string { return b.data.String() }

func (b *documentationOutput) Write(data []byte) (int, error) {
	if len(data) > (1<<20)-b.data.Len() {
		return 0, fmt.Errorf("documentation command output exceeds 1 MiB")
	}
	return b.data.Write(data)
}

func documentationCommand(t *testing.T, dir string, environment []string, executable string, args ...string) string {
	t.Helper()
	output, err := documentationResult(t, dir, environment, executable, args...)
	if err != nil {
		t.Fatalf("offline documentation command %v: %v\n%s\nCache the project toolchain and dependencies explicitly before this package check.", args, err, output)
	}
	return output
}

func documentationResult(t *testing.T, dir string, environment []string, executable string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir, command.Env, command.WaitDelay = dir, environment, time.Second
	var output documentationOutput
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	return output.String(), err
}

func documentationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%v %d", info.Mode(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			file, err := os.Open(name)
			if err != nil {
				return err
			}
			digest := sha256.New()
			_, err = io.Copy(digest, file)
			file.Close()
			if err != nil {
				return err
			}
			value += fmt.Sprintf(" %x", digest.Sum(nil))
		}
		result[strings.TrimPrefix(name, root)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
