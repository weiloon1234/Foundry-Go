package consumer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Retired originals remain inputs to the catalogue validator, never the compiler.
// The source hashes make adding a case to this exception an explicit review.
func activeCompilerCases(t *testing.T) []compilerCase {
	t.Helper()
	data, err := os.ReadFile("testdata/compilefail/retired.json")
	if err != nil {
		t.Fatal(err)
	}
	var retired struct {
		Cases map[string]string `json:"cases"`
	}
	if err := json.Unmarshal(data, &retired); err != nil {
		t.Fatal(err)
	}
	active := []compilerCase{}
	for _, c := range compilerCases() {
		hash, ok := retired.Cases[c.name]
		if !ok {
			active = append(active, c)
			continue
		}
		source, err := os.ReadFile(filepath.Join("testdata/compilefail", c.name, "invalid.go"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != hash {
			t.Fatalf("retired catalogue input changed: %s", c.name)
		}
		delete(retired.Cases, c.name)
	}
	if len(retired.Cases) != 0 {
		t.Fatalf("retirement lists unknown compiler cases: %v", retired.Cases)
	}
	return active
}
func TestRetiredCompilerInputsNeverEnterBatches(t *testing.T) {
	data, err := os.ReadFile("testdata/compilefail/retired.json")
	if err != nil {
		t.Fatal(err)
	}
	var retired struct {
		Cases map[string]string `json:"cases"`
	}
	if err := json.Unmarshal(data, &retired); err != nil {
		t.Fatal(err)
	}
	for _, c := range activeCompilerCases(t) {
		if _, ok := retired.Cases[c.name]; ok {
			t.Fatalf("retired input reached compiler batch: %s", c.name)
		}
	}
	if len(compilerCases())-len(activeCompilerCases(t)) != len(retired.Cases) {
		t.Fatal("retirement removed an unrelated compiler case")
	}
}
