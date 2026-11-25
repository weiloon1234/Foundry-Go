package docsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

// TypeScriptInputs names the main strict fixture’s compiler input manifest.
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
		data, err := markdown.ReadFile(root, manifest, markdown.MaxManifestBytes)
		if err != nil {
			return err
		}
		locations := markdown.NewLines(data)
		var names []string
		if err := json.Unmarshal(data, &names); err != nil {
			line := 0
			var syntax *json.SyntaxError
			var valueType *json.UnmarshalTypeError
			var offset int64
			switch {
			case errors.As(err, &syntax):
				offset = syntax.Offset
			case errors.As(err, &valueType):
				offset = valueType.Offset
			}
			if offset > 0 {
				line = locations.Number(int(offset - 1))
			}
			return diagnostic.DocInputs.At(manifest, line, 0).Say("compiler input JSON", "syntax or type error").To("write a JSON array of TypeScript filenames").Caused(err)
		}
		offset := 0
		for _, name := range names {
			line := 0
			if index := bytes.Index(data[offset:], []byte(strconv.Quote(name))); index >= 0 {
				offset += index
				line = locations.Number(offset)
				offset += len(strconv.Quote(name))
			}
			if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".d.ts") {
				return diagnostic.DocInputs.At(manifest, line, 0).Say(fmt.Sprintf("compiler input %q", name), "not a handwritten .ts file of this directory").To("use a handwritten .ts filename from this fixture directory").Err()
			}
			source := directory + "/" + name
			if inputs[source] {
				return diagnostic.DocInputs.At(manifest, line, 0).Say(fmt.Sprintf("compiler input %q", name), "listed twice").To("remove the duplicate manifest entry").Err()
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
	data, err := markdown.ReadAll("fixture compiler inputs", input, markdown.MaxManifestBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
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
			return diagnostic.DocInputs.At("fixture compiler inputs", 0, 0).Say("JSON", fmt.Sprintf("invalid near byte %d", decoder.InputOffset())).To("regenerate the list with go list -json in the fixture module").Caused(err)
		}
		if pkg.Incomplete || pkg.Error != nil || !filepath.IsAbs(pkg.Dir) {
			return diagnostic.DocInputs.At("fixture compiler inputs", 0, 0).Say(fmt.Sprintf("package %q", pkg.Dir), "incomplete").To("regenerate the input list with go list -json in the fixture module").Err()
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
				return diagnostic.DocInputs.At(inclusion.Document, inclusion.Line, 0).Say(inclusion.Source+"#"+inclusion.Region, "not a compiler input").To("move the example into a selected fixture source or add it to the fixture’s compiler inputs").Err()
			}
		}
	}
	return nil
}
