package oracle

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// ComparisonAudit is a reviewed census of fixture test sources, independent of
// recorder call discovery. Tests names the paired legacy/new entry points. Empty
// Tests means that source has no paired golden assertion (ordinary behavioral,
// integration, compiler and harness tests retain their own assertions).
// Source changes/new files require reviewing the census, not simply discovering
// fewer recorder calls. This live-capture audit retires with the legacy code.
type ComparisonAudit struct {
	Files map[string]AuditedSource `json:"files"`
}
type AuditedSource struct {
	PostgresTests []string `json:"postgres_tests,omitempty"`
	SHA256        string   `json:"sha256"`
	Tests         []string `json:"paired_tests,omitempty"`
}

func TestSources(fixtures string) (map[string]string, error) {
	found := map[string]string{}
	for _, module := range []string{"goway", "consumer"} {
		err := filepath.WalkDir(filepath.Join(fixtures, module), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(fixtures, path)
			if err != nil {
				return err
			}
			found[filepath.ToSlash(rel)] = Digest(data)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}
func (a ComparisonAudit) Validate(fixtures string) error {
	sources, err := TestSources(fixtures)
	if err != nil {
		return err
	}
	expected := map[string]string{}
	for file, s := range a.Files {
		expected[file] = s.SHA256
		for _, test := range s.PostgresTests {
			if !slices.Contains(s.Tests, test) {
				return fmt.Errorf("PostgreSQL comparison is not in paired tests: %s %s", file, test)
			}
		}
	}
	if !reflect.DeepEqual(sources, expected) {
		return fmt.Errorf("legacy comparison census is stale: review changed/added/removed test sources before capture")
	}
	return nil
}
func (a ComparisonAudit) Packages(fixtures string) map[string][]string {
	groups := map[string][]string{}
	for file, s := range a.Files {
		if len(s.Tests) > 0 {
			dir := filepath.Join(fixtures, filepath.Dir(file))
			groups[dir] = append(groups[dir], s.Tests...)
		}
	}
	return groups
}

func (a ComparisonAudit) PostgreSQL(fixtures string) map[string]map[string]bool {
	groups := map[string]map[string]bool{}
	for file, s := range a.Files {
		dir := filepath.Join(fixtures, filepath.Dir(file))
		if groups[dir] == nil {
			groups[dir] = map[string]bool{}
		}
		for _, test := range s.PostgresTests {
			groups[dir][test] = true
		}
	}
	return groups
}

// CompareCapture permits an offline check to omit only explicitly registered
// PostgreSQL tests. Missing ordinary tests/sites must still require a refreeze.
func (a ComparisonAudit) CompareCapture(previous, captured Bundle, includePostgres bool) error {
	for id, next := range captured.Records {
		before, ok := previous.Records[id]
		if !ok || next.Kind != before.Kind || next.Package != before.Package || next.Site != before.Site || next.Transcript != before.Transcript {
			return fmt.Errorf("legacy observation %s changed; explicit --refreeze required", id)
		}
	}
	for id, before := range previous.Records {
		if _, ok := captured.Records[id]; ok {
			continue
		}
		omitted := false
		if !includePostgres && before.Kind == "golden" {
			name, err := recordTest(id, before.Package, before.Site)
			if err != nil {
				return err
			}
			root, _, _ := strings.Cut(name, "/")
			for file, source := range a.Files {
				if filepath.ToSlash(filepath.Dir(file)) == before.Package && slices.Contains(source.PostgresTests, root) {
					omitted = true
					break
				}
			}
		}
		if !omitted {
			return fmt.Errorf("legacy observation %s removed; explicit --refreeze required", id)
		}
	}
	return nil
}
