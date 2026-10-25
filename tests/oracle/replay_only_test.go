package oracle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplayOnlyMatchesLegacy(t *testing.T) {
	dir := pending(t)
	b := sample()
	id := "p/" + t.Name() + "/site/0"
	var r Record
	for _, record := range b.Records {
		r = record
		break
	}
	r.Package = "p"
	r.Site = "site"
	r.Kind = "golden"
	r.Transcript = Snapshot([]any{"SELECT $1", int64(7)})
	b.Records = map[string]Record{id: r}
	if err := Publish(dir, b, false); err != nil {
		t.Fatal(err)
	}
	replay := NewReplayAt("p", dir)
	if !replay.Match(t, "site", Snapshot([]any{"SELECT $1", int64(7)})) {
		t.Fatal("matching legacy observation rejected")
	}
	if !replay.consumed[id] {
		t.Fatal("observation not counted")
	}
}

func TestReplayOnlyFailsClosed(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for mode, diagnostic := range map[string]string{
		"pending": "replay-only assertion requires frozen evidence",
		"capture": "replay-only assertion cannot capture a legacy observation",
		"changed": "current output differs",
		"missing": "missing frozen oracle",
	} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestReplayOnlyFailureChild$")
			ConfigureCommand(cmd)
			cmd.Env = append(os.Environ(), "FOUNDRY_ORACLE_FAILURE_CHILD="+mode)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("replay accepted %s: %s", mode, output)
			}
			if !strings.Contains(string(output), diagnostic) {
				t.Fatalf("wrong rejection for %s: %s", mode, output)
			}
		})
	}
}
func TestReplayOnlyFailureChild(t *testing.T) {
	mode := os.Getenv("FOUNDRY_ORACLE_FAILURE_CHILD")
	if mode == "" {
		return
	}
	dir := pending(t)
	if mode == "capture" {
		t.Setenv("FOUNDRY_ORACLE_STAGE", t.TempDir())
	}
	if mode == "changed" || mode == "missing" {
		b := sample()
		var record Record
		for _, r := range b.Records {
			record = r
			break
		}
		record.Package = "p"
		record.Site = "site"
		record.Kind = "golden"
		record.Transcript = "legacy"
		b.Records = map[string]Record{"p/" + t.Name() + "/site/0": record}
		if err := Publish(dir, b, false); err != nil {
			t.Fatal(err)
		}
	}
	site := "site"
	if mode == "missing" {
		site = "absent"
	}
	if !NewReplayAt("p", dir).Match(t, site, "current") {
		t.Fatal("current output differs")
	}
}

func TestInventoryIncludesReplayOnlySites(t *testing.T) {
	dir := t.TempDir()
	source := `package example
 func TestNative(t *testing.T){oracleMatch(t,"native/site",current)}
 `
	if err := os.WriteFile(filepath.Join(dir, "native_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	sites, names, err := Inventory(dir)
	if err != nil || !sites["native/site"] || len(names) != 1 || names[0] != "TestNative" {
		t.Fatal(sites, names, err)
	}
}
