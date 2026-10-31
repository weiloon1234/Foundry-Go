package clientcontracts_test

import (
	"bytes"
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

const currentContractCohort = "testdata/current-contracts"

func TestPublishClientContracts(t *testing.T) {
	if os.Getenv("FOUNDRY_PUBLISH_CLIENT_CONTRACTS") != "1" {
		t.Skip("explicit make publish-client-contracts only")
	}
	pending, err := os.MkdirTemp("testdata", ".client-publication-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(pending)
	for _, name := range []string{"client", "reporting"} {
		cohortDir := filepath.Join(pending, name)
		if err := os.Mkdir(cohortDir, 0755); err != nil {
			t.Fatal(err)
		}
		source, selections := contractBaselineSource(t, name)
		options := typescript.Options{
			Dir:      cohortDir,
			Prefix:   "contracts",
			OpenAPI:  openapi.Options{Title: "Consumer", APIVersion: "1"},
			React:    true,
			Vue:      true,
			Surfaces: selections,
		}
		if _, err := typescript.Generate(t.Context(), source, options); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		if _, err := typescript.Generate(t.Context(), source, options); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(cohortDir, ".foundry-gen.json")
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		checkOwnershipHashes(t, cohortDir, manifestData)
	}

	baseCohort := currentContractCohort
	if _, err := os.Stat(currentContractCohort); os.IsNotExist(err) {
		baseCohort = nativeCursorCohort
	} else if err != nil {
		t.Fatal(err)
	}

	artifacts := map[string]contractHashChange{}
	for _, name := range []string{"client", "reporting"} {
		oldDir := filepath.Join(baseCohort, name)
		newDir := filepath.Join(pending, name)

		nameSet := map[string]bool{}
		for _, art := range artifactNames(t, newDir) {
			nameSet[art] = true
		}
		if _, err := os.Stat(oldDir); err == nil {
			for _, art := range artifactNames(t, oldDir) {
				nameSet[art] = true
			}
		}
		names := make([]string, 0, len(nameSet))
		for art := range nameSet {
			names = append(names, art)
		}
		slices.Sort(names)

		for _, art := range names {
			rel := filepath.Join(name, art)
			var oldHash, newHash string
			oldPath := filepath.Join(oldDir, art)
			if data, err := os.ReadFile(oldPath); err == nil {
				oldHash = artifactHash(data)
			}
			newPath := filepath.Join(newDir, art)
			if data, err := os.ReadFile(newPath); err == nil {
				newHash = artifactHash(data)
			}
			artifacts[rel] = contractHashChange{Old: oldHash, New: newHash}
		}
	}

	command := exec.Command("git", "rev-parse", "HEAD")
	commit, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	evidence := cursorPublication{
		Commit:    strings.TrimSpace(string(commit)),
		Command:   "make publish-client-contracts",
		Sources:   map[string]string{},
		Artifacts: artifacts,
	}
	for _, path := range []string{"client_publication_test.go", "contracts.go", "reporting_contracts_test.go"} {
		evidence.Sources[path] = artifactHash(readArtifact(t, path))
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pending, "provenance.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	target := currentContractCohort
	if _, err := os.Stat(target); err == nil {
		backup, err := os.MkdirTemp("testdata", ".current-contracts-backup-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(target, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(pending, target); err != nil {
			_ = os.Rename(backup, target)
			t.Fatal(err)
		}
		_ = os.RemoveAll(backup)
	} else if os.IsNotExist(err) {
		if err := os.Rename(pending, target); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
}

func TestReleasedContractCohorts(t *testing.T) {
	t.Run("ProvenanceVerificationHarness", func(t *testing.T) {
		testVerifyCohortProvenanceHarness(t)
	})
	cohorts, err := filepath.Glob(filepath.Join("testdata", "contracts-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cohort := range cohorts {
		info, err := os.Stat(cohort)
		if err != nil || !info.IsDir() {
			continue
		}
		t.Run(filepath.Base(cohort), func(t *testing.T) {
			if err := verifyCohortProvenance(cohort); err != nil {
				t.Errorf("%s: %v", cohort, err)
			}
		})
	}
}

func testVerifyCohortProvenanceHarness(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	cohortDir := filepath.Join(tempDir, "contracts-v1.0.0")
	if err := os.MkdirAll(filepath.Join(cohortDir, "client"), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("// test artifact content\n")
	artName := "contracts_foundry.gen.ts"
	artPath := filepath.Join(cohortDir, "client", artName)
	if err := os.WriteFile(artPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	artHash := artifactHash(content)

	manifestData := []byte(fmt.Sprintf("{\n  \"version\": 2,\n  \"files\": {\n    %q: %q\n  }\n}\n", artName, artHash))
	manifestPath := filepath.Join(cohortDir, "client", ".foundry-gen.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatal(err)
	}
	manifestHash := artifactHash(manifestData)

	evidence := cursorPublication{
		Commit:  "abcdef123456",
		Command: "make publish-client-contracts",
		Sources: map[string]string{"foo.go": "bar"},
		Artifacts: map[string]contractHashChange{
			"client/.foundry-gen.json":        {Old: "oldhash", New: manifestHash},
			"client/contracts_foundry.gen.ts": {Old: "oldhash", New: artHash},
		},
	}
	pData, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	provenancePath := filepath.Join(cohortDir, "provenance.json")
	if err := os.WriteFile(provenancePath, append(pData, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Valid self-consistent cohort passes.
	if err := verifyCohortProvenance(cohortDir); err != nil {
		t.Fatalf("expected valid cohort to pass: %v", err)
	}

	t.Run("DeletedArtifact", func(t *testing.T) {
		relative := "client/contracts_optional_foundry.gen.ts"
		deletedPath := filepath.Join(cohortDir, relative)
		evidence.Artifacts[relative] = contractHashChange{Old: artHash, New: ""}
		t.Cleanup(func() {
			delete(evidence.Artifacts, relative)
			if err := os.WriteFile(provenancePath, append(pData, '\n'), 0644); err != nil {
				t.Error(err)
			}
		})
		deletionData, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(provenancePath, append(deletionData, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyCohortProvenance(cohortDir); err != nil {
			t.Fatalf("expected recorded artifact deletion to pass: %v", err)
		}
		for _, kind := range []string{"file", "directory", "dangling-symlink"} {
			t.Run(kind, func(t *testing.T) {
				switch kind {
				case "file":
					err = os.WriteFile(deletedPath, content, 0644)
				case "directory":
					err = os.Mkdir(deletedPath, 0755)
				case "dangling-symlink":
					err = os.Symlink("missing-artifact", deletedPath)
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Remove(deletedPath); err != nil {
						t.Error(err)
					}
				})
				if err := verifyCohortProvenance(cohortDir); err == nil {
					t.Fatal("expected artifact recorded as deleted but still present to fail")
				}
			})
		}
	})

	// 2. Tampered artifact file is detected.
	if err := os.WriteFile(artPath, []byte("// tampered!\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCohortProvenance(cohortDir); err == nil {
		t.Fatal("expected tampered artifact content to fail")
	}
	if err := os.WriteFile(artPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Tampered provenance hash is detected.
	tamperedEvidence := evidence
	tamperedEvidence.Artifacts["client/contracts_foundry.gen.ts"] = contractHashChange{Old: "oldhash", New: "wronghash"}
	badPData, _ := json.MarshalIndent(tamperedEvidence, "", "  ")
	if err := os.WriteFile(provenancePath, append(badPData, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCohortProvenance(cohortDir); err == nil {
		t.Fatal("expected tampered provenance hash to fail")
	}
	if err := os.WriteFile(provenancePath, append(pData, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// 4. Missing artifact file is detected.
	if err := os.Remove(artPath); err != nil {
		t.Fatal(err)
	}
	if err := verifyCohortProvenance(cohortDir); err == nil {
		t.Fatal("expected missing artifact to fail")
	}
	if err := os.WriteFile(artPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// 5. Unrecorded extra artifact file is detected.
	extraPath := filepath.Join(cohortDir, "client", "contracts_extra_foundry.gen.ts")
	if err := os.WriteFile(extraPath, []byte("extra"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCohortProvenance(cohortDir); err == nil {
		t.Fatal("expected unrecorded artifact file to fail")
	}
	_ = os.Remove(extraPath)

	if err := verifyCohortProvenance(cohortDir); err != nil {
		t.Fatalf("expected restored cohort to pass: %v", err)
	}
}

func verifyCohortProvenance(cohort string) error {
	provenancePath := filepath.Join(cohort, "provenance.json")
	data, err := os.ReadFile(provenancePath)
	if err != nil {
		return fmt.Errorf("missing provenance for released cohort %s: %w", cohort, err)
	}
	var evidence cursorPublication
	if err := json.Unmarshal(data, &evidence); err != nil {
		return fmt.Errorf("invalid provenance JSON in %s: %w", cohort, err)
	}
	if evidence.Commit == "" || evidence.Command == "" || len(evidence.Artifacts) == 0 {
		return fmt.Errorf("incomplete provenance in %s", cohort)
	}
	for relative, change := range evidence.Artifacts {
		actualPath := filepath.Join(cohort, relative)
		if change.New == "" {
			if _, err := os.Lstat(actualPath); err == nil {
				return fmt.Errorf("artifact recorded as deleted still present in %s: %s", cohort, relative)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("checking deleted artifact in %s: %s (%w)", cohort, relative, err)
			}
			continue
		}
		fileData, err := os.ReadFile(actualPath)
		if err != nil {
			return fmt.Errorf("artifact listed in provenance missing from %s: %s (%w)", cohort, relative, err)
		}
		actualHash := artifactHash(fileData)
		if actualHash != change.New {
			return fmt.Errorf("provenance hash mismatch in %s for %s: recorded %s, actual %s", cohort, relative, change.New, actualHash)
		}
	}
	for _, sub := range []string{"client", "reporting"} {
		subDir := filepath.Join(cohort, sub)
		if _, err := os.Stat(subDir); err != nil {
			continue
		}
		files, err := filepath.Glob(filepath.Join(subDir, "*_foundry.gen.*"))
		if err != nil {
			return err
		}
		names := []string{".foundry-gen.json"}
		for _, file := range files {
			names = append(names, filepath.Base(file))
		}
		for _, name := range names {
			rel := filepath.Join(sub, name)
			if _, ok := evidence.Artifacts[rel]; !ok {
				return fmt.Errorf("unrecorded artifact found on disk in %s: %s", cohort, rel)
			}
		}
		manifestPath := filepath.Join(subDir, ".foundry-gen.json")
		mData, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("missing ownership manifest in %s: %w", subDir, err)
		}
		if err := validateOwnershipHashes(subDir, mData); err != nil {
			return fmt.Errorf("ownership validation failed in %s: %w", subDir, err)
		}
	}
	return nil
}

func validateOwnershipHashes(dir string, data []byte) error {
	var ownership struct {
		Version int               `json:"version"`
		Files   map[string]string `json:"files"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ownership); err != nil {
		return err
	}
	if ownership.Version != 2 {
		return fmt.Errorf("unexpected ownership version: %d", ownership.Version)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*_foundry.gen.*"))
	if err != nil {
		return err
	}
	if len(ownership.Files) != len(files) {
		return fmt.Errorf("ownership manifest file count mismatch: got %d, want %d", len(ownership.Files), len(files))
	}
	for _, file := range files {
		name := filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		expectedHash := artifactHash(data)
		if ownership.Files[name] != expectedHash {
			return fmt.Errorf("ownership hash mismatch for %s: manifest %s, file %s", name, ownership.Files[name], expectedHash)
		}
	}
	return nil
}
