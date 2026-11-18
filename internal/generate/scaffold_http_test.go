package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDTOScaffoldHTTPDirective(t *testing.T) {
	for _, test := range []struct{ http, role, directive string }{
		{"", "", "//foundry:dto"},
		{"body", "", "//foundry:dto http=body"},
		{"response", "", "//foundry:dto http=response"},
		{"both", "", "//foundry:dto http=both"},
		{"response", "response", "//foundry:dto role=response http=response"},
		{"both", "response", "//foundry:dto role=response http=both"},
		{"", "response", "//foundry:dto role=response"},
	} {
		t.Run(test.http+"/"+test.role, func(t *testing.T) {
			dir := fixture(t, "package sample\n")
			path, err := scaffoldFixture(t.Context(), ScaffoldOptions{Dir: dir, Kind: DTOScaffold, Name: "SaveRequest", HTTP: test.http, Role: test.role})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var directive string
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "//foundry:dto") {
					directive = line
				}
			}
			if directive != test.directive {
				t.Fatalf("directive %q; want %q", directive, test.directive)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("scaffold published generated output")
			}
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDTOScaffoldHTTPRejectsBeforeWriting(t *testing.T) {
	for _, test := range []struct{ name, http, source, testSource, message string }{
		{"unknown", "invalid", "package sample\n", "", "--http"},
		{"none is omitted", "none", "package sample\n", "", "--http"},
		{"body collision", "body", "package sample\nfunc SaveBody(){}\n", "", "SaveBody"},
		{"response collision", "response", "package sample\nvar SaveResponse int\n", "", "SaveResponse"},
		{"both collision", "both", "package sample\nfunc SaveResponse(){}\n", "", "SaveResponse"},
		{"test collision", "body", "package sample\n", "package sample\nfunc SaveBody(){}\n", "SaveBody"},
		{"planned collision", "body", "package sample\n//foundry:dto http=body\ntype SaveResource struct{}\n", "", "SaveBody"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := fixture(t, test.source)
			if test.testSource != "" {
				write(t, dir, "collision_test.go", test.testSource)
			}
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			path, err := scaffoldFixture(t.Context(), ScaffoldOptions{Dir: dir, Kind: DTOScaffold, Name: "SaveRequest", HTTP: test.http})
			if path != "" || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatal(path, err)
			}
			after, err := os.ReadDir(dir)
			if err != nil || len(before) != len(after) {
				t.Fatal("rejection wrote files", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "save_request_dto.go")); !os.IsNotExist(err) {
				t.Fatal("scaffold was published", err)
			}
		})
	}
}
