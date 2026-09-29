package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func TestEnumCasesExcludeAliasesHelpersAndIgnoredConstants(t *testing.T) {
	dir := fixture(t, `package sample

//foundry:enum
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	statusHidden   Status = "hidden" //foundry:ignore
	//foundry:ignore
	StatusUnknown Status = "unknown"
)

// DefaultStatus is an alias of an existing case, not another wire value.
const DefaultStatus = StatusActive
const LegacyStatus Status = Status(StatusDisabled)

//foundry:enum
type Level int

const (
	LevelLow Level = iota
	LevelHigh
	//foundry:ignore
	levelCount
)

// defaultLevel is an unexported alias of a case, not another wire value.
const defaultLevel = LevelLow
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)
	status := output["status_foundry.gen.go"]
	if !strings.Contains(status, "return []Status{StatusActive, StatusDisabled}") {
		t.Fatalf("unexpected status cases:\n%s", status)
	}
	for _, excluded := range []string{"statusHidden", "StatusUnknown", "DefaultStatus", "LegacyStatus"} {
		if strings.Contains(status, excluded) {
			t.Fatalf("%s became an enum case", excluded)
		}
	}
	if level := output["level_foundry.gen.go"]; !strings.Contains(level, "return []Level{LevelLow, LevelHigh}") || strings.Contains(level, "levelCount") {
		t.Fatalf("unexported sentinel became a case:\n%s", level)
	}
}

// An unexported constant with its own value used to be a case. Dropping it
// silently would change the wire contract, so generation explains both fixes.
func TestEnumUnexportedValueRequiresExportOrIgnore(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:enum\ntype Level int\nconst (\n LevelLow Level = iota\n LevelHigh\n levelCount\n)\n")
	_, err := Generate(t.Context(), Options{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "models.go:7:2:") || !strings.Contains(err.Error(), "unexported constant levelCount of enum Level") || !strings.Contains(err.Error(), "//foundry:ignore") {
		t.Fatalf("unexported enum value lacked an actionable diagnostic: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("rejected enum published output")
	}
}

func TestEnumDuplicateValuesExplainTheFix(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:enum\ntype Status string\nconst (\n A Status = \"same\"\n B Status = \"same\"\n)\n")
	_, err := Generate(t.Context(), Options{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "models.go:6:2:") || !strings.Contains(err.Error(), "duplicate serialized values: A and B") || !strings.Contains(err.Error(), "//foundry:ignore") {
		t.Fatalf("duplicate enum value lacked an actionable diagnostic: %v", err)
	}
}

func TestGeneratedHeaderOmitsLineAndDiagnosticsUseModulePaths(t *testing.T) {
	dir := fixture(t, sampleSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["user_foundry.gen.go"]
	if !strings.Contains(output, "// Source: models.go.\n") {
		t.Fatalf("header lost its source file or kept a line number:\n%s", output[:120])
	}
	// Adding lines above a declaration must not make generated output stale.
	write(t, dir, "models.go", "// Package sample documents its models.\n\n"+sampleSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatalf("line movement made output stale: %v", err)
	}
	nested := filepath.Join(dir, "domain")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, nested, "user.go", "package domain\n//foundry:model table=accounts\ntype Account struct{Name string}\n")
	if _, err := Generate(t.Context(), Options{Dir: nested}); err == nil || !strings.Contains(err.Error(), "domain/user.go:3:") {
		t.Fatalf("diagnostic lacked its module-relative path: %v", err)
	}
}

func TestCheckListsStaleFilesAndReasons(t *testing.T) {
	dir := fixture(t, sampleSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "user_foundry.gen.go: missing") || !strings.Contains(err.Error(), manifestName+": ownership manifest differs") {
		t.Fatalf("fresh check lacked missing files: %v", err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "models.go", strings.Replace(sampleSource, "Name string;", "Name string; Age int;", 1))
	report, err := Generate(t.Context(), Options{Dir: dir, Check: true})
	if err == nil || !strings.Contains(err.Error(), "user_foundry.gen.go: content differs") {
		t.Fatalf("content change was not explained: %v", err)
	}
	if len(report.Stale) == 0 || report.Stale[len(report.Stale)-1].Path != "user_foundry.gen.go" {
		t.Fatalf("stale report = %+v", report.Stale)
	}
	write(t, dir, "models.go", strings.Replace(sampleSource, "//foundry:enum\n", "", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "status_foundry.gen.go: obsolete") {
		t.Fatalf("obsolete output was not explained: %v", err)
	}
	source := []byte("package sample\n\n// A.\nfunc A() {}\n")
	for _, test := range []struct {
		after []byte
		want  StaleReason
	}{
		{[]byte("package sample\n// A.\nfunc A()  {}\n"), StaleFormatting},
		{[]byte("package sample\n\n// B.\nfunc A() {}\n"), StaleComments},
		{[]byte("package sample\n\n// A.\nfunc B() {}\n"), StaleContent},
	} {
		plan := writePlan{changes: map[string][]byte{"a_foundry.gen.go": test.after}, before: map[string]oldFile{"a_foundry.gen.go": {exists: true, data: source}}}
		if got := staleReason("a_foundry.gen.go", plan); got != test.want {
			t.Errorf("staleReason = %s, want %s", got, test.want)
		}
	}
}

func TestGeneratedSymbolsRejectHandwrittenCollisions(t *testing.T) {
	for name, test := range map[string]struct{ source, want string }{
		"function": {"func UserFields() {}\n", "models.go:15:6: UserFields is declared by handwritten code"},
		"variable": {"var UserDraftAlias, UserFieldsAt = 1, 2\n", "models.go:15:21: UserFieldsAt is declared by handwritten code"},
		"method":   {"func (Status) IsValid() bool { return true }\n", "models.go:15:15: Status.IsValid is declared by handwritten code"},
		"dto":      {"//foundry:dto\ntype Reply struct{Name string}\nfunc ReplyValidationFields() {}\n", "ReplyValidationFields is declared by handwritten code"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, sampleSource+test.source)
			_, err := Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "rename the handwritten declaration") {
				t.Fatalf("collision was not reported at its source: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("collision published output")
			}
		})
	}
}

func TestDescriptorsAreCachedAndResponseDTOsOmitValidation(t *testing.T) {
	dir := fixture(t, `package sample

//foundry:enum
type Status string

const Active Status = "active"

//foundry:dto
type Request struct{ Name string }

//foundry:dto role=response
type Reply struct{ Name string; State Status }

//foundry:path pattern=/items/{id}
type ItemPath struct{ ID int }

//foundry:query
type Search struct{ Term string }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)
	for name, want := range map[string]string{
		"status_foundry.gen.go":    "var foundryStatusEnumDescriptor = sync.OnceValue(",
		"request_foundry.gen.go":   "var foundryRequestValidationFields = sync.OnceValue(",
		"reply_foundry.gen.go":     "var foundryReplyJSON = sync.OnceValue(",
		"item_path_foundry.gen.go": "var foundryItemPathDescriptor = sync.OnceValue(",
		"search_foundry.gen.go":    "var foundrySearchDescriptor = sync.OnceValue(",
	} {
		if !strings.Contains(output[name], want) {
			t.Errorf("%s lacks cached descriptor %q", name, want)
		}
	}
	if strings.Contains(output["reply_foundry.gen.go"], "ValidationField") {
		t.Fatal("response-only DTO generated request validation descriptors")
	}
	write(t, dir, "models.go", "package sample\n//foundry:dto role=request\ntype Reply struct{Name string}\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "role=response") {
		t.Fatalf("unknown DTO role accepted: %v", err)
	}
}

func TestFrameworkVersionMismatchFailsBeforeWriting(t *testing.T) {
	selected := &listedModule{Path: framework, Version: "v0.2.0"}
	for _, test := range []struct {
		name    string
		build   frameworkinfo.Build
		module  *listedModule
		failure bool
	}{
		{"match", frameworkinfo.Build{Version: "v0.2.0"}, selected, false},
		{"mismatch", frameworkinfo.Build{Version: "v0.3.0"}, selected, true},
		{"development tool", frameworkinfo.Build{}, selected, false},
		{"local replacement", frameworkinfo.Build{Version: "v0.3.0"}, &listedModule{Path: framework, Version: "v0.2.0", Replace: &listedModule{Path: "../framework"}}, false},
		{"versioned replacement", frameworkinfo.Build{Version: "v0.3.0"}, &listedModule{Path: framework, Version: "v0.2.0", Replace: &listedModule{Path: "fork.example/framework", Version: "v0.3.0"}}, false},
		{"framework module", frameworkinfo.Build{Version: "v0.3.0"}, &listedModule{Path: framework, Main: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := &packageGraph{framework: test.module}
			err := g.checkFramework(test.build)
			if (err != nil) != test.failure || (err != nil && !strings.Contains(err.Error(), "go tool foundry")) {
				t.Fatalf("checkFramework = %v", err)
			}
		})
	}
}
