package docsource

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const TypeScriptInputs = "tests/fixtures/consumer/clientcontracts/typescript-inputs.json"

// CheckTypeScriptInputs uses the same file list the fresh-client strict compiler
// copies into its project. Frozen clients and unregistered testdata are not proof.
func CheckTypeScriptInputs(root string, documents []Document) error {
	inputs := make(map[string]bool)
	manifests := make(map[string]string)
	for _, doc := range documents {
		for _, inclusion := range doc.Inclusions {
			if !strings.HasSuffix(inclusion.Source, ".ts") {
				continue
			}
			directory := filepath.ToSlash(filepath.Dir(inclusion.Source))
			manifest := directory + "/typescript-inputs.json"
			if directory == "tests/fixtures/consumer/clientcontracts/testdata" {
				manifest = TypeScriptInputs
			}
			manifests[manifest] = directory
		}
	}
	for manifest, directory := range manifests {
		data, err := os.ReadFile(filepath.Join(root, manifest))
		if err != nil {
			return err
		}
		var names []string
		if err := json.Unmarshal(data, &names); err != nil {
			return err
		}
		for _, name := range names {
			if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".d.ts") {
				return fmt.Errorf("%s: invalid compiler input %q", manifest, name)
			}
			source := directory + "/" + name
			if inputs[source] {
				return fmt.Errorf("%s: repeated compiler input %q", manifest, name)
			}
			inputs[source] = true
		}
	}
	return checkInputs(documents, ".ts", inputs)
}

// CheckGoInputs consumes concatenated go list -json ./... results from the exact
// fixture modules and build environment used by make fixture-check. Test files
// count because that gate uses go test, not just go build. IgnoredGoFiles do not.
func CheckGoInputs(root string, documents []Document, input io.Reader) error {
	inputs := make(map[string]bool)
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(input)
	for {
		var pkg struct {
			Dir                                          string
			GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
			Incomplete                                   bool
			Error                                        *struct{ Err string }
		}
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("fixture compiler inputs: %w", err)
		}
		if pkg.Incomplete || pkg.Error != nil || !filepath.IsAbs(pkg.Dir) {
			return fmt.Errorf("fixture compiler inputs: incomplete package %q", pkg.Dir)
		}
		for _, group := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles} {
			for _, file := range group {
				relative, err := filepath.Rel(root, filepath.Join(pkg.Dir, file))
				if err != nil {
					return err
				}
				inputs[filepath.ToSlash(relative)] = true
			}
		}
	}
	return checkInputs(documents, ".go", inputs)
}

func checkInputs(documents []Document, extension string, inputs map[string]bool) error {
	for _, document := range documents {
		for _, inclusion := range document.Inclusions {
			if strings.HasSuffix(inclusion.Source, extension) && !inputs[inclusion.Source] {
				return fmt.Errorf("%s:%d: %s#%s is not a compiler input", inclusion.Document, inclusion.Line, inclusion.Source, inclusion.Region)
			}
		}
	}
	return nil
}
