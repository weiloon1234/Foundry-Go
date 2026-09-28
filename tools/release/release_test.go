package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func sourceFixture(t *testing.T) (string, sourcePolicy) {
	t.Helper()
	root := t.TempDir()
	module, err := readModule(".")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := policyFor("../..")
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "internal/frameworkinfo/module.go", "package frameworkinfo\nconst GenerationManifest = "+strconv.Quote(policy.generationManifest)+"\n")
	put(t, root, "go.mod", "module example.test/Framework\n\ngo "+module.Go.Version+"\n")
	put(t, root, "go.sum", "")
	put(t, root, "LICENSE", "fixture license")
	put(t, root, "record.go", "package framework\n")
	put(t, root, policy.generationManifest, `{"version":1,"files":{}}`)
	put(t, root, ".env.test", "PRIVATE-SENTINEL")
	put(t, root, ".cache/private.json", "PRIVATE-SENTINEL")
	put(t, root, "node_modules/private.json", "PRIVATE-SENTINEL")
	for _, item := range []struct{ dir, name string }{
		{"tests/fixtures/plugin_base", "pluginbase"}, {"tests/fixtures/plugin_dep", "plugindep"}, {"tests/fixtures/consumer", "consumer"},
	} {
		put(t, root, item.dir+"/go.mod", "module foundry.test/"+item.name+"\n\ngo "+module.Go.Version+"\n\nrequire example.test/Framework v0.0.0\nreplace example.test/Framework => ../../..\n")
		put(t, root, item.dir+"/go.sum", "")
		put(t, root, item.dir+"/fixture.go", "package "+item.name+"\n")
	}
	put(t, root, "tests/fixtures/consumer/productionprofile/record.go", "package productionprofile\n")
	put(t, root, "tests/fixtures/consumer/configuredprofile/routes.go", "package configuredprofile\n")
	put(t, root, "tests/fixtures/consumer/localization/messages.go", "package localization\n")
	put(t, root, "tests/fixtures/consumer/localization/locales/ms/messages.json", "{\"welcome\":\"Selamat datang\"}\n")
	return root, policy
}

func TestPrivateCandidateHasCanonicalIndependentModulesAndPreservesOwnership(t *testing.T) {
	root, policy := sourceFixture(t)
	var first manifest
	for round := range 2 {
		out := filepath.Join(t.TempDir(), "candidate")
		if err := prepare(options{root: root, out: out, version: "v0.0.0-candidate.24"}); err != nil {
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
		if len(report.Artifacts) != 3 || len(report.Consumers) != 3 || report.Format != 2 {
			t.Fatal("incomplete release manifest")
		}
		for i, item := range report.Artifacts {
			archive, err := zip.OpenReader(filepath.Join(out, filepath.FromSlash(item.Zip)))
			if err != nil {
				t.Fatal(err)
			}
			var ownership, license bool
			for _, file := range archive.File {
				if strings.Contains(file.Name, ".env") || strings.Contains(file.Name, ".cache/") || strings.Contains(file.Name, "node_modules/") || strings.Contains(file.Name, "tests/fixtures/") {
					t.Fatal("private or nested source leaked", file.Name)
				}
				ownership = ownership || strings.HasSuffix(file.Name, "/"+policy.generationManifest)
				license = license || strings.HasSuffix(file.Name, "/LICENSE")
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if !license || (i == 0 && !ownership) || !strings.HasPrefix(item.Sum, "h1:") {
				t.Fatal("artifact lost license, ownership, or hash")
			}
			if round == 1 && (item.SHA256 != first.Artifacts[i].SHA256 || item.Sum != first.Artifacts[i].Sum) {
				t.Fatal("identical source did not produce identical module bytes")
			}
		}
		for _, name := range []string{"full", "ordinary", "configured"} {
			file, err := readModule(filepath.Join(out, "consumers", name))
			if err != nil {
				t.Fatal(err)
			}
			if len(file.Replace) != 0 || file.Require[0].Mod.Version != "v0.0.0-candidate.24" {
				t.Fatal("consumer still depends on workspace replacement")
			}
		}
		for _, name := range []string{"productionprofile/record.go", "configuredprofile/routes.go", "localization/messages.go", "localization/locales/ms/messages.json"} {
			if _, err := os.Stat(filepath.Join(out, "consumers/configured", name)); err != nil {
				t.Fatal("configured profile lost required source", err)
			}
		}
		for _, name := range []string{"configuredprofile", "localization"} {
			if _, err := os.Stat(filepath.Join(out, "consumers/ordinary", name)); !os.IsNotExist(err) {
				t.Fatal("ordinary profile acquired configured fixture source", name)
			}
		}
		if err := prepare(options{root: root, out: out, version: "v0.0.0-candidate.24"}); err == nil {
			t.Fatal("existing candidate overwritten")
		}
		current, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
		if !bytes.Equal(current, data) {
			t.Fatal("failed retry mutated accepted artifact")
		}
		first = report
	}
	data, _ := os.ReadFile(filepath.Join(root, "tests/fixtures/consumer/go.mod"))
	if !bytes.Contains(data, []byte("replace example.test/Framework => ../../..")) {
		t.Fatal("source consumer was modified")
	}
}

func TestReleaseRejectsUnknownAssetsLinksAndReplacements(t *testing.T) {
	for _, mode := range []string{"link", "asset", "replace"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := sourceFixture(t)
			switch mode {
			case "link":
				if err := os.Symlink(filepath.Join(root, ".env.test"), filepath.Join(root, "linked.go")); err != nil {
					t.Fatal(err)
				}
			case "asset":
				put(t, root, "unknown.pem", "PRIVATE-SENTINEL")
			case "replace":
				file, err := os.OpenFile(filepath.Join(root, "tests/fixtures/consumer/go.mod"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString("replace example.test/unexpected => /private/source\n")
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
			if err := prepare(options{root: root, out: filepath.Join(t.TempDir(), "candidate"), version: "v0.0.0-candidate.24"}); err == nil {
				t.Fatal("unsafe or incoherent source accepted")
			}
		})
	}
}
