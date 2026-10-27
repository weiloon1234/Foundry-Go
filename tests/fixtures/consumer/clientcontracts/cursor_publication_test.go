package clientcontracts_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

const nativeCursorCohort = "testdata/native-cursor-contracts"
const historicalCursorCohort = "testdata/datatable-baseline"

// The retired ID is historical comparison data, never a live descriptor.
const historicalCursorID = "github.com/weiloon1234/Foundry-Go/database/query.Cursor["
const publishedCursorID = "github.com/weiloon1234/Foundry-Go/database/orm.Cursor["

var cursorChangedArtifacts = []string{
	"client/.foundry-gen.json",
	"client/contracts_foundry.gen.ts",
	"client/contracts_manifest_foundry.gen.json",
	"client/contracts_members_foundry.gen.ts",
	"client/contracts_openapi_foundry.gen.json",
}

type contractHashChange struct {
	Old string `json:"old_sha256"`
	New string `json:"new_sha256"`
}

type cursorPublication struct {
	Commit    string                        `json:"base_commit"`
	Command   string                        `json:"command"`
	Sources   map[string]string             `json:"source_sha256"`
	Artifacts map[string]contractHashChange `json:"artifacts"`
}

func TestPublishCursorContracts(t *testing.T) {
	if os.Getenv("FOUNDRY_PUBLISH_CURSOR_CONTRACTS") != "1" {
		t.Skip("explicit make publish-cursor-contracts only")
	}
	pending, err := os.MkdirTemp("testdata", ".cursor-publication-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(pending)
	for _, name := range []string{"client", "reporting"} {
		if err := os.Mkdir(filepath.Join(pending, name), 0755); err != nil {
			t.Fatal(err)
		}
		source, selections := contractBaselineSource(t, name)
		options := typescript.Options{Dir: filepath.Join(pending, name), Prefix: "contracts", OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}, React: true, Vue: true, Surfaces: selections}
		if _, err := typescript.Generate(t.Context(), source, options); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		if _, err := typescript.Generate(t.Context(), source, options); err != nil {
			t.Fatal(err)
		}
	}
	changes := checkCursorPublication(t, pending)
	if t.Failed() {
		t.FailNow()
	}
	command := exec.Command("git", "rev-parse", "HEAD")
	commit, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	evidence := cursorPublication{Commit: strings.TrimSpace(string(commit)), Command: "make publish-cursor-contracts", Sources: map[string]string{}, Artifacts: changes}
	for _, path := range []string{"../../../../http/pagination/cursor_request.go", "cursor_publication_test.go", "reporting_contracts_test.go", "contracts.go"} {
		evidence.Sources[path] = artifactHash(readArtifact(t, path))
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pending, "provenance.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	// Publication is a new cohort, not an overwrite of captured history. Reruns
	// accept the same bytes and leave the original publication provenance intact.
	if _, err := os.Stat(nativeCursorCohort); err == nil {
		for _, name := range []string{"client", "reporting"} {
			assertContractBytes(t, filepath.Join(pending, name), filepath.Join(nativeCursorCohort, name))
		}
		checkCursorPublication(t, nativeCursorCohort)
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Rename(pending, nativeCursorCohort); err != nil {
		t.Fatal(err)
	}
}

func TestCursorContractPublication(t *testing.T) {
	changes := checkCursorPublication(t, nativeCursorCohort)
	var evidence cursorPublication
	if err := json.Unmarshal(readArtifact(t, filepath.Join(nativeCursorCohort, "provenance.json")), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Commit == "" || evidence.Command != "make publish-cursor-contracts" || len(evidence.Sources) == 0 {
		t.Fatal("publication provenance missing")
	}
	if len(changes) != len(evidence.Artifacts) {
		t.Fatal("artifact provenance set differs")
	}
	for name, change := range changes {
		if evidence.Artifacts[name] != change {
			t.Errorf("protected artifact hash changed: %s", name)
		}
	}
}

func checkCursorPublication(t *testing.T, current string) map[string]contractHashChange {
	t.Helper()
	changes := map[string]contractHashChange{}
	var changed []string
	for _, cohort := range []string{"client", "reporting"} {
		oldNames := artifactNames(t, filepath.Join(historicalCursorCohort, cohort))
		newNames := artifactNames(t, filepath.Join(current, cohort))
		if !slices.Equal(oldNames, newNames) {
			t.Fatalf("artifact set changed for %s: %v != %v", cohort, oldNames, newNames)
		}
		for _, name := range oldNames {
			relative := filepath.Join(cohort, name)
			before := readArtifact(t, filepath.Join(historicalCursorCohort, relative))
			after := readArtifact(t, filepath.Join(current, relative))
			changes[relative] = contractHashChange{Old: artifactHash(before), New: artifactHash(after)}
			if !bytes.Equal(before, after) {
				changed = append(changed, relative)
			}
			if name == ".foundry-gen.json" {
				checkOwnershipHashes(t, filepath.Join(current, cohort), after)
				continue
			}
			expected := bytes.ReplaceAll(before, []byte(historicalCursorID), []byte(publishedCursorID))
			if !bytes.Equal(expected, after) {
				t.Errorf("unexpected protected artifact difference: %s", relative)
			}
		}
	}
	slices.Sort(changed)
	if !slices.Equal(changed, cursorChangedArtifacts) {
		t.Fatalf("changed protected artifacts: %v; want %v", changed, cursorChangedArtifacts)
	}
	return changes
}

func checkOwnershipHashes(t *testing.T, dir string, data []byte) {
	t.Helper()
	var ownership struct {
		Version int               `json:"version"`
		Files   map[string]string `json:"files"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ownership); err != nil {
		t.Fatal(err)
	}
	if ownership.Version != 2 {
		t.Fatalf("unexpected ownership version: %d", ownership.Version)
	}
	names := artifactNames(t, dir)
	if len(ownership.Files) != len(names)-1 {
		t.Fatal("ownership manifest file set changed")
	}
	for _, name := range names {
		if name != ".foundry-gen.json" && ownership.Files[name] != artifactHash(readArtifact(t, filepath.Join(dir, name))) {
			t.Errorf("ownership hash mismatch: %s", name)
		}
	}
}

func artifactNames(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_foundry.gen.*"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{".foundry-gen.json"}
	for _, file := range files {
		names = append(names, filepath.Base(file))
	}
	slices.Sort(names)
	return names
}
func readArtifact(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func artifactHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
