package generate

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// ConversionFile is an exact, proved before/after replacement in one package.
// Handwritten sources remain consumer-owned after conversion.
type ConversionFile struct {
	Name          string
	Before, After []byte
}

// PublishConversion publishes a proved conversion through ordinary generated
// ownership, guarded writes and recovery. Callers must supply every source read
// by the proof, including unchanged sources, for the locked freshness check.
func PublishConversion(ctx context.Context, dir string, files []ConversionFile) error {
	return publishConversion(ctx, dir, files, false)
}

// ValidateConversion checks the same source and generated-output ownership plan
// as publication without acquiring locks or writing files.
func ValidateConversion(ctx context.Context, dir string, files []ConversionFile) error {
	return publishConversion(ctx, dir, files, true)
}

func publishConversion(ctx context.Context, dir string, files []ConversionFile, check bool) (resultErr error) {
	defer func() { resultErr = ownSourceDiagnostic(resultErr) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	input := &packageInput{dir: absolute, previous: map[string][]byte{}}
	input.goNames, err = goSourceNames(absolute)
	if err != nil {
		return err
	}
	outputs := map[string][]byte{}
	oldManifest, err := readFile(filepath.Join(absolute, manifestName))
	if err != nil {
		return err
	}
	if oldManifest.exists {
		owned, err := decodeManifestPolicy(oldManifest.data, goOutputPolicy)
		if err != nil {
			return err
		}
		for name := range owned.Files {
			old, err := readFile(filepath.Join(absolute, name))
			if err != nil {
				return err
			}
			if old.exists {
				input.previous[name] = old.data
				outputs[name] = old.data
			}
		}
	}
	seen := map[string]bool{}
	var sourceNames []string
	for _, file := range files {
		if seen[file.Name] || filepath.Base(file.Name) != file.Name {
			return safeerror.New("invalid conversion file name")
		}
		seen[file.Name] = true
		if strings.HasSuffix(file.Name, ".go") && file.Before != nil {
			sourceNames = append(sourceNames, file.Name)
		}
		if file.Name == manifestName {
			continue
		}
		if outputName.MatchString(file.Name) {
			if file.After == nil {
				delete(outputs, file.Name)
			} else {
				outputs[file.Name] = file.After
			}
		} else if strings.HasSuffix(file.Name, ".go") && !strings.HasPrefix(file.Name, ".") && file.Before != nil && bytes.Equal(file.Before, file.After) {
			// Test and inactive sources are freshness inputs, never conversion targets.
		} else if !conversionSource(file.Name) || file.Before == nil || file.After == nil {
			return safeerror.New("conversion requires existing non-test Go source")
		}
	}
	slices.Sort(sourceNames)
	if !slices.Equal(sourceNames, input.goNames) {
		return safeerror.New("Go source file set changed since conversion proof")
	}
	plan, err := planWrite(input, outputs)
	if err != nil {
		return err
	}
	plan.conversion = true
	for _, file := range files {
		old, err := readFile(filepath.Join(absolute, file.Name))
		if err != nil {
			return err
		}
		if old.exists != (file.Before != nil) || !bytes.Equal(old.data, file.Before) {
			return safeerror.New("source changed since conversion proof: " + file.Name)
		}
		if file.Name == manifestName {
			expected := old.data
			if next, changed := plan.changes[manifestName]; changed {
				expected = next
			}
			if !bytes.Equal(expected, file.After) {
				return safeerror.New("conversion generated ownership differs from its proof")
			}
		} else if !outputName.MatchString(file.Name) && !bytes.Equal(file.Before, file.After) {
			plan.changes[file.Name] = bytes.Clone(file.After)
		}
		plan.before[file.Name] = old
	}
	if check || len(plan.changes) == 0 {
		return nil
	}
	return publishBatch(ctx, absolute, []packageWrite{{input, plan}}, nil, nil)
}

func conversionSource(name string) bool {
	return filepath.Base(name) == name && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !outputName.MatchString(name)
}
