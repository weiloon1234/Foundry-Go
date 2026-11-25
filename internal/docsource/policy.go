package docsource

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Policy decides which source files a document may bind and words the findings
// that name where examples live. The framework binds its independent fixtures;
// an application binds its own code.
type Policy interface {
	// approve returns the source language of a bindable file or the reason the
	// document may not bind it.
	approve(root, document, name string) (language string, err error)
	// hostsTSX reports whether a tsx fence may carry a binding.
	hostsTSX() bool
	// requiresWorkedExample reports whether a worked-example region is checked.
	requiresWorkedExample() bool
	words() policyWords
}

type policyWords struct {
	// bindHint finishes the finding for an executable block without a binding.
	bindHint string
	// renderFix finishes the finding for a block that differs from its source.
	renderFix string
	// rebind ends a finding whose source or region cannot be used; empty for the
	// framework, whose sentence ends there.
	rebind string
	// inputs names the compiler-input list and says how to put a file on it.
	inputs, inputsFix string
}

// sentenceEnd closes a sentence that rebind may extend.
func (w policyWords) sentenceEnd() string {
	if w.rebind == "" {
		return "."
	}
	return w.rebind
}

type frameworkPolicy struct{}

// FrameworkPolicy binds the handwritten files of the independent consumer fixtures.
func FrameworkPolicy() Policy { return frameworkPolicy{} }

func (frameworkPolicy) hostsTSX() bool              { return false }
func (frameworkPolicy) requiresWorkedExample() bool { return false }
func (frameworkPolicy) words() policyWords {
	return policyWords{
		bindHint:  "move the code into a fixture file and bind it with <!-- compiled-source: path#region -->.",
		renderFix: "run go run ./internal/cmd/renderdocs",
		inputs:    "fixture compiler inputs",
		inputsFix: "move the example into a selected fixture source or add it to the fixture’s compiler inputs",
	}
}

func (frameworkPolicy) approve(_, _, name string) (string, error) {
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, `\:#`) || strings.HasPrefix(name, "/") {
		return "", safeerror.New(fmt.Sprintf("invalid repository-relative source path %q; use a clean path below tests/fixtures", name))
	}
	ext := path.Ext(name)
	approved := (strings.HasPrefix(name, "tests/fixtures/consumer/") || strings.HasPrefix(name, "tests/fixtures/goway/")) && ext == ".go"
	if ext == ".ts" {
		approved = (path.Dir(name) == "tests/fixtures/consumer/clientcontracts/testdata" ||
			path.Dir(name) == "tests/fixtures/consumer/clientcontracts/testdata/table-bindings" ||
			path.Dir(name) == "tests/fixtures/consumer/clientcontracts/testdata/limits") && !strings.HasSuffix(name, ".d.ts")
	}
	if !approved || strings.Contains(name, "_foundry.gen.") {
		return "", safeerror.New(fmt.Sprintf("source %q is outside approved handwritten fixture inputs; bind a handwritten file of an independent fixture", name))
	}
	if ext == ".go" {
		return "go", nil
	}
	return "typescript", nil
}

type applicationPolicy struct{}

// ApplicationPolicy binds an application's own handwritten code: Go under
// internal/, cmd/ and examples/, TypeScript and TSX under web/ and client/tests/.
func ApplicationPolicy() Policy { return applicationPolicy{} }

func (applicationPolicy) hostsTSX() bool              { return true }
func (applicationPolicy) requiresWorkedExample() bool { return true }
func (applicationPolicy) words() policyWords {
	return policyWords{
		bindHint:  "move the code into a handwritten file under internal/, cmd/, examples/, web/ or client/tests/ and bind it with <!-- compiled-source: path#region -->, then run go tool foundry docs render --dir .",
		renderFix: "run go tool foundry docs render --dir .",
		rebind:    ", or point the binding at the application's own file and region (add // doc-example:begin and // doc-example:end markers there), then run go tool foundry docs render --dir .",
		inputs:    "module packages",
		inputsFix: "bind a file of a package of this module that the default build compiles (no build tag, not generated)",
	}
}

const testingSkill = ".claude/skills/testing/"

var applicationGoRoots = []string{"internal/", "cmd/", "examples/"}
var applicationTypeScriptRoots = []string{"web/", "client/tests/"}

// Directory names an application never binds: dependencies, build output,
// test data, deployment and configuration trees.
var applicationRefusedDirectories = map[string]bool{
	"node_modules": true, "dist": true, "testdata": true, "deploy": true, "config": true,
}

func (applicationPolicy) approve(root, document, name string) (string, error) {
	refuse := func(reason string) (string, error) {
		return "", safeerror.New(fmt.Sprintf("source %q is not bindable: %s", name, reason))
	}
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, `\:#`) || !filepath.IsLocal(name) {
		return "", safeerror.New(fmt.Sprintf("invalid module-relative source path %q; use a clean path below internal/, cmd/, examples/, web/ or client/tests/", name))
	}
	segments := strings.Split(name, "/")
	for _, segment := range segments {
		if strings.HasPrefix(segment, ".env") {
			return refuse("it names a secret input")
		}
	}
	var language string
	roots := applicationGoRoots
	switch path.Ext(name) {
	case ".go":
		language = "go"
	case ".ts":
		language, roots = "typescript", applicationTypeScriptRoots
	case ".tsx":
		language, roots = "tsx", applicationTypeScriptRoots
	default:
		return refuse("bindable files are .go under internal/, cmd/ and examples/, and .ts or .tsx under web/ and client/tests/")
	}
	if strings.HasSuffix(name, ".d.ts") {
		return refuse("a declaration file is not handwritten teaching code")
	}
	if strings.Contains(name, "_foundry.gen.") || name == "client/generated" || strings.HasPrefix(name, "client/generated/") {
		return refuse("generated output is not handwritten; bind the declaration it is generated from")
	}
	for _, segment := range segments[:len(segments)-1] {
		if applicationRefusedDirectories[segment] {
			return refuse("files under " + segment + " are not bindable")
		}
	}
	inRoot := false
	for _, prefix := range roots {
		inRoot = inRoot || strings.HasPrefix(name, prefix)
	}
	if !inRoot {
		if language == "go" {
			return refuse("Go sources are bindable under internal/, cmd/ and examples/ only")
		}
		return refuse("TypeScript and TSX sources are bindable under web/ and client/tests/ only")
	}
	if language == "go" && strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") {
		if !strings.HasPrefix(document, testingSkill) {
			return refuse("a test file is bindable only from the testing skill (" + testingSkill + "SKILL.md)")
		}
	}
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		// An unreadable directory is reported when the source itself is opened.
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(dir), "go.mod")); err == nil {
			return refuse("it belongs to the nested module " + dir)
		}
	}
	return language, nil
}
