package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic input here tests the integrity protocol, never a SQL expectation.
func sample() Bundle {
	sources := map[string]string{"source.go": strings.Repeat("b", 64)}
	data, _ := JSON(sources)
	return Bundle{Version: Version, Sources: sources, Records: map[string]Record{"p/TestGolden/site/0": {Package: "p", Site: "site", Kind: "golden", Transcript: "synthetic integrity-test payload", Provenance: Provenance{Commit: strings.Repeat("a", 40), Sources: Digest(data), Command: []string{"go", "test"}, Toolchain: "test-toolchain", Platform: "test-platform", WorkingDirectory: "/fixture", Build: BuildEnvironment{GOFLAGS: "-p=4", GOWORK: "off", GOENV: "", GOTOOLCHAIN: "local", GOOS: "test", GOARCH: "test", GOVERSION: "test", GOROOT: "/go", CGO_ENABLED: "0", GOMOD: "/fixture/go.mod"}}}}}
}
func pending(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	data, _ := JSON(State{Version: Version, Phase: "pending"})
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestPublicationRequiresExplicitRefreeze(t *testing.T) {
	dir := pending(t)
	b := sample()
	if err := Publish(dir, b, true); err == nil {
		t.Fatal("refreeze accepted pending evidence")
	}
	if err := Publish(dir, b, false); err != nil {
		t.Fatal(err)
	}
	if err := Publish(dir, b, false); err == nil {
		t.Fatal("implicit overwrite accepted")
	}
	if err := Publish(dir, b, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
}
func TestGuardRejectsChangedMissingOrUnprovenancedRecords(t *testing.T) {
	for _, kind := range []string{"changed", "missing", "provenance", "empty", "receipt"} {
		t.Run(kind, func(t *testing.T) {
			dir := pending(t)
			b := sample()
			if err := Publish(dir, b, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "records.json")
			switch kind {
			case "changed":
				data, _ := os.ReadFile(path)
				data = append(data, ' ')
				os.WriteFile(path, data, 0600)
			case "missing":
				os.Remove(path)
			case "receipt":
				os.Remove(filepath.Join(dir, "state.json"))
			default:
				if kind == "empty" {
					b.Records = map[string]Record{}
				} else {
					r := b.Records["p/TestGolden/site/0"]
					r.Provenance.Sources = ""
					b.Records["p/TestGolden/site/0"] = r
				}
				data, _ := JSON(b)
				os.WriteFile(path, data, 0600)
				// Even a recomputed digest cannot bless absent provenance.
				receipt, _ := JSON(State{Version: Version, Phase: "frozen", SHA256: Digest(data), Command: "go run ./cmd/freeze-oracles --refreeze"})
				os.WriteFile(filepath.Join(dir, "state.json"), receipt, 0600)
			}
			if _, err := Load(dir); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
func TestPendingRejectsUnreceiptedEvidence(t *testing.T) {
	dir := pending(t)
	data, _ := JSON(sample())
	os.WriteFile(filepath.Join(dir, "records.json"), data, 0600)
	if _, err := Load(dir); err == nil {
		t.Fatal("pending state accepted fabricated records")
	}
}
func TestCountSymbols(t *testing.T) {
	f, d, a, err := CountSymbols([]byte("123 T hello\n456 t world\n789 D data\n U external\n"))
	if err != nil || f != 2 || d != 3 || a != 0 {
		t.Fatal(f, d, a, err)
	}
	if _, _, _, err := CountSymbols([]byte("bad symbol format\n")); err == nil {
		t.Fatal("bad nm accepted")
	}
}

func TestReplacementCanAddAndRemoveSitesAfterReplay(t *testing.T) {
	published := pending(t)
	before := sample()
	if err := Publish(published, before, false); err != nil {
		t.Fatal(err)
	}
	stage := pending(t)
	replacement := sample()
	record := replacement.Records["p/TestGolden/site/0"]
	delete(replacement.Records, "p/TestGolden/site/0")
	record.Site = "added"
	replacement.Records["p/TestNew/added/0"] = record
	if err := Publish(stage, replacement, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(stage)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := SelectTests("", "")
	if err != nil {
		t.Fatal(err)
	}
	if missing, err := Unconsumed(loaded, "p", nil, selection); err != nil || len(missing) != 1 {
		t.Fatal(missing, err)
	}
	if missing, err := Unconsumed(loaded, "p", map[string]bool{"p/TestNew/added/0": true}, selection); err != nil || len(missing) != 0 {
		t.Fatal(missing, err)
	}
	if err := Publish(published, loaded, true); err != nil {
		t.Fatal(err)
	}
	final, err := Load(published)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := final.Records["p/TestGolden/site/0"]; ok {
		t.Fatal("removed site survived replacement")
	}
}
