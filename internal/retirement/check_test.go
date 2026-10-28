package retirement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredDataLayerAbsent(t *testing.T) {
	if err := Check("../.."); err != nil {
		t.Fatal(err)
	}
}

func TestRetirementGuardRejectsReintroduction(t *testing.T) {
	imported := framework + retiredPath
	cases := map[string]struct{ path, source string }{
		"directory":                   {retiredPath + "/", ""},
		"ordinary":                    {"sample/a.go", `package sample; import "` + imported + `"`},
		"test":                        {"sample/a_test.go", `package sample; import alias "` + imported + `"`},
		"ignored":                     {"sample/a.go", "//go:build ignore\n\npackage sample; import . \"" + imported + "\""},
		"compile fail":                {"testdata/compilefail/a.go", `package sample; import _ "` + imported + `"`},
		"nested cache":                {"sample/.cache/a.go", `package sample; import "` + imported + `"`},
		"escaped":                     {"sample/a.go", `package sample; import "` + strings.Replace(imported, "query", `\x71uery`, 1) + `"`},
		"tool":                        {"tools/a.go", `package sample; import "` + imported + `/sub"`},
		"nested module":               {"fixture/a.go", `package sample; import "` + imported + `"`},
		"filename":                    {"sample/other_legacy.go", `package sample`},
		"type":                        {"sample/a.go", `package sample; type LegacyType int`},
		"function":                    {"sample/a.go", `package sample; func LegacyCall(){}`},
		"field":                       {"sample/a.go", `package sample; type T struct { LegacyField int }`},
		"parameter":                   {"sample/a.go", `package sample; func f[LegacyParam any](){}`},
		"deprecated":                  {"sample/a.go", "package sample\n// Deprecated: old.\nfunc Call(){}"},
		"grouped":                     {"sample/a.go", "package sample\n/* Deprecated: old. */\ntype (Exported int)"},
		"spec":                        {"sample/a.go", "package sample\ntype (\n// Deprecated: old.\nExported int\n)"},
		"method":                      {"sample/a.go", "package sample\ntype T int\n// Deprecated: old.\nfunc (T) Call(){}"},
		"deprecated embedded field":   {"sample/a.go", "package sample\ntype Embedded struct{}\ntype Public struct {\n// Deprecated: old.\nEmbedded\n}"},
		"deprecated embedded pointer": {"sample/a.go", "package sample\ntype Embedded[T any] struct{}\ntype Public struct {\n// Deprecated: old.\n*Embedded[int]\n}"},
		"alias":                       {"sample/a.go", `package sample; import q "` + imported + `"; type A[T any] = q.Query[T]; type B[T any] = A[T]`},
		"bridge":                      {"database/orm/neutral.go", `package orm; func Extension(){}`},
		"dot bridge alias":            {"sample/a.go", `package sample; import . "` + framework + `testkit/factory"; type Renamed = Model[int]`},
		"imported bridge alias":       {"sample/a.go", `package sample; import old "` + framework + `testkit/factory"; type Renamed = old.Model[int]`},
		"bridge alias":                {"testkit/factory/neutral.go", `package factory; type Model = int`},
		"parenthesized receiver":      {"database/orm/neutral.go", `package orm; type Predicate int; func (p (Predicate)) Not(){}`},
		"forwarding method":           {"database/orm/neutral.go", `package orm; type Predicate int; func (Predicate) Not(){}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n")
			if name == "nested module" {
				writeSource(t, root, "fixture/go.mod", "module fixture\n")
			}
			if strings.HasSuffix(tc.path, "/") {
				if err := os.MkdirAll(filepath.Join(root, tc.path), 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeSource(t, root, tc.path, tc.source)
			}
			if err := Check(root); err == nil {
				t.Fatal("reintroduction accepted")
			}
		})
	}
}

func TestRetirementGuardAcceptsNativeAndHistoricalText(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module example\n")
	writeSource(t, root, "testdata/archive/a.go.txt", `package old; type LegacyType int`)
	writeSource(t, root, ".cache/x/a.go", `package old; import "`+framework+retiredPath+`"`)
	writeSource(t, root, "native/a.go", `package native; const Historical = "LegacyName"; type Native int`)
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
}

func writeSource(t *testing.T, root, path, source string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRetirementDenialLedger(t *testing.T) {
	denied, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	for owner, names := range denied {
		for _, name := range names {
			t.Run(owner+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n")
				source := "package sample; "
				if receiver, method, ok := strings.Cut(name, "."); ok {
					source += "type " + receiver + " struct{}; func (" + receiver + ") " + method + "(){}"
				} else {
					source += "type " + name + " int"
				}
				writeSource(t, root, owner+"/neutral.go", source)
				if err := Check(root); err == nil {
					t.Fatal("denial ledger entry not enforced")
				}
			})
		}
	}
}

func TestRetirementGuardRejectsDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module example\n")
	if err := os.MkdirAll(filepath.Join(root, "database"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, retiredPath)); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err == nil {
		t.Fatal("retired package symlink accepted")
	}
}

func TestRetirementGuardPublicMethodSets(t *testing.T) {
	cases := []struct {
		name, declaration, implementation string
		denied                            bool
	}{
		{"generic alias", "type Predicate[T any] = renamed[T]", "type renamed[T any] int; func (renamed[T]) Not() {}", true},
		{"pointer alias", "type Predicate[T any] = renamed[T]", "type renamed[T any] int; func (*renamed[T]) Not() {}", true},
		{"alias chain", "type intermediate[T any] = renamed[T]; type Predicate[T any] = intermediate[T]", "type renamed[T any] int; func (*renamed[T]) Not() {}", true},
		{"promoted value", "type Predicate[T any] struct { renamed[T] }", "type renamed[T any] int; func (renamed[T]) Not() {}", true},
		{"promoted pointer", "type Predicate[T any] struct { renamed[T] }", "type renamed[T any] int; func (*renamed[T]) Not() {}", true},
		{"embedded pointer", "type Predicate[T any] struct { *renamed[T] }", "type renamed[T any] int; func (*renamed[T]) Not() {}", true},
		{"unrelated type", "type Predicate[T any] struct{}; type Unrelated[T any] = renamed[T]", "type renamed[T any] int; func (*renamed[T]) Not() {}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n")
			writeSource(t, root, "database/orm/types.go", "package orm; "+tc.implementation)
			// These declarations must be checked even when ordinary Go package
			// discovery would omit the file. The method lives in another file.
			writeSource(t, root, "database/orm/exposed.go", "//go:build ignore\n\npackage orm; "+tc.declaration)
			err := Check(root)
			if tc.denied {
				if err == nil || !strings.Contains(err.Error(), "retired public selector "+framework+"database/orm.Predicate.Not") {
					t.Fatalf("expected exposed method denial, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unrelated method rejected: %v", err)
			}
		})
	}
}

func TestRetirementGuardExposures(t *testing.T) {
	cases := []struct {
		name, owner, source, want string
	}{
		{"deprecated embedding through alias", "sample", "type Embedded struct{}; type hidden struct {\n// Deprecated: removed embedding.\nEmbedded\n}; type Public = hidden", "deprecated exposed declaration"},
		{"variable method", "database/orm", "type hidden struct{}; func (hidden) Not() {}; var Predicate = hidden{}", "retired public selector " + framework + "database/orm.Predicate.Not"},
		{"variable pointer method", "database/orm", "type hidden struct{}; func (*hidden) Not() {}; var Predicate hidden", "retired public selector " + framework + "database/orm.Predicate.Not"},
		{"constant method", "database/orm", "type hidden int; func (hidden) Not() {}; const Predicate hidden = 0", "retired public selector " + framework + "database/orm.Predicate.Not"},
		{"variable interface method", "database/orm", "var Predicate interface { Not() }", "retired public selector " + framework + "database/orm.Predicate.Not"},
		{"alias field", "datatable", "type hidden struct { DownloadTimeout int }; type Config = hidden", "retired public selector " + framework + "datatable.Config.DownloadTimeout"},
		{"promoted field", "datatable", "type hidden struct { DownloadTimeout int }; type Config struct { hidden }", "retired public selector " + framework + "datatable.Config.DownloadTimeout"},
		{"pointer promoted field", "datatable", "type hidden struct { DownloadTimeout int }; type Config struct { *hidden }", "retired public selector " + framework + "datatable.Config.DownloadTimeout"},
		{"variable field", "datatable", "var Config struct { DownloadTimeout int }", "retired public selector " + framework + "datatable.Config.DownloadTimeout"},
		{"deprecated hidden type", "sample", "// Deprecated: hidden type.\ntype hidden struct{}; type Public = hidden", "deprecated exposed declaration"},
		{"deprecated hidden method", "sample", "type hidden struct{}\n// Deprecated: hidden method.\nfunc (hidden) Call() {}\nvar Public hidden", "deprecated exposed declaration"},
		{"deprecated promoted field", "sample", "type hidden struct {\n// Deprecated: hidden field.\nField int\n}; type Public struct { hidden }", "deprecated exposed declaration"},
		{"deprecated interface method", "sample", "type hidden interface {\n// Deprecated: hidden method.\nCall()\n}; var Public hidden", "deprecated exposed declaration"},
		{"deprecated interface embedding", "sample", "type other interface { Call() }; type hidden interface {\n// Deprecated: embedded contract.\nother\n}; type Public = hidden", "deprecated exposed declaration"},
		{"deprecated inline interface embedding", "sample", "type other interface { Call() }; var Public interface {\n// Deprecated: embedded contract.\nother\n}", "deprecated exposed declaration"},
		{"deprecated result", "sample", "type hidden struct {\n// Deprecated: hidden field.\nField int\n}; func Public() hidden { return hidden{} }", "deprecated exposed declaration"},
		{"deprecated variable", "sample", "// Deprecated: variable.\nvar Public int", "deprecated exposed declaration"},
		{"deprecated constant", "sample", "// Deprecated: constant.\nconst Public = 1", "deprecated exposed declaration"},
		{"deprecated function", "sample", "// Deprecated: function.\nfunc Public() {}", "deprecated exposed declaration"},
		{"unexposed declaration", "sample", "type hidden struct {\n// Deprecated: private API.\nField int\n}; type Public struct { private hidden }", ""},
		{"unrelated selector", "datatable", "type Config struct{}; var Other struct { DownloadTimeout int }", ""},
		{"ambiguous promotion", "datatable", "type left struct { DownloadTimeout int }; type right struct { DownloadTimeout int }; type Config struct { left; right }", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n")
			writeSource(t, root, tc.owner+"/source.go", "package sample\n"+tc.source)
			err := Check(root)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRetirementGuardImportedDeprecation(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module example\n")
	writeSource(t, root, "helper/helper.go", "package helper\ntype Hidden struct {\n// Deprecated: removed field.\nField int\n}")
	writeSource(t, root, "api/api.go", "package api; import \"example/helper\"; type Public = helper.Hidden")
	if err := Check(root); err == nil || !strings.Contains(err.Error(), "example/api.Public.Field") {
		t.Fatalf("imported declaration was not checked at its exposure: %v", err)
	}
}

func TestRetirementGuardNestedInterfaceDeprecations(t *testing.T) {
	for name, source := range map[string]string{
		"nested":         "type leaf interface { Call() }; type hidden interface {\n// Deprecated: embedded contract.\nleaf\n}; type Public interface { hidden }",
		"generic origin": "type leaf[T any] interface { Call(T) }; type hidden[T any] interface {\n// Deprecated: embedded contract.\nleaf[T]\n}; type middle[T any] interface { hidden[T] }; type Public interface { middle[int] }",
		"generic alias":  "type leaf[T any] interface { Call(T) }; type hidden[T any] interface {\n// Deprecated: embedded contract.\nleaf[T]\n}; type alias[T any] = hidden[T]; type Public interface { alias[int] }",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module example\n")
			writeSource(t, root, "sample.go", "package sample\n"+source)
			err := Check(root)
			if err == nil || !strings.Contains(err.Error(), "deprecated exposed declaration example.Public") {
				t.Fatalf("nested interface annotation was not checked: %v", err)
			}
		})
	}
}

func TestRetirementGuardBuildVariants(t *testing.T) {
	cases := []struct {
		name, owner, ordinary, variant, shared string
		denied                                 bool
	}{
		{"field", "datatable", "type Config struct{}", "type Config struct { DownloadTimeout int }", "", true},
		{"alias field", "datatable", "type Config struct{}", "type Config = hidden; type hidden struct { DownloadTimeout int }", "", true},
		{"cross-file alias chain", "datatable", "type Config struct{}", "type Config = middle; type hidden struct { DownloadTimeout int }", "type middle = hidden", true},
		{"method", "database/orm", "type Predicate[T any] struct{}", "type Predicate[T any] struct{}; func (*Predicate[T]) Not() {}", "", true},
		{"alias method", "database/orm", "type Predicate[T any] struct{}", "type Predicate[T any] = hidden[T]; type hidden[T any] struct{}; func (*hidden[T]) Not() {}", "", true},
		{"interface method", "database/orm", "type Predicate interface{}", "type Predicate interface { Not() }", "", true},
		{"benign duplicates", "datatable", "type Config struct { Timeout int }", "type Config struct { Timeout string }", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n")
			writeSource(t, root, tc.owner+"/a_default.go", "//go:build !custom\n\npackage sample\n"+tc.ordinary)
			writeSource(t, root, tc.owner+"/z_custom.go", "//go:build custom\n\npackage sample\n"+tc.variant)
			if tc.shared != "" {
				writeSource(t, root, tc.owner+"/shared.go", "package sample\n"+tc.shared)
			}
			err := Check(root)
			if tc.denied {
				if err == nil || !strings.Contains(err.Error(), "declaration independent of build selection") {
					t.Fatalf("variant declaration was not denied syntactically: %v", err)
				}
			} else if err != nil {
				t.Fatalf("legitimate mutually exclusive declarations rejected: %v", err)
			}
		})
	}
}
