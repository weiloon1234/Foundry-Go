package consumer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const compilerBatchSize = 12
const compilerBatchTimeout = 2 * time.Minute
const compilerBatchOutputBytes = 8 << 20
const compilerPackagePrefix = "foundry.test/consumer/testdata/compilefail/"

var compilerCaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
var compilerDiagnosticLine = regexp.MustCompile(`^(.*\.go):[0-9]+(?::[0-9]+)?: (.*)$`)

type compilerCase struct{ name, diagnostic string }
type compilerOutcome struct {
	output string
	err    error
}

func TestInvalidPublicContractsFailCompilation(t *testing.T) {
	testkit.TrackExternalInputs(t)
	checkCompilerCases(t, compilerCases(), runCompilerBatches)
}

func checkCompilerCases(t *testing.T, cases []compilerCase, run func(context.Context, []compilerCase) map[string]compilerOutcome) {
	t.Helper()
	seen := make(map[string]bool, len(cases))
	for _, item := range cases {
		if !compilerCaseName.MatchString(item.name) || item.diagnostic == "" || seen[item.name] {
			t.Fatal("invalid or duplicate compiler catalogue entry", item.name)
		}
		seen[item.name] = true
	}
	ctx := t.Context()
	var selected []compilerCase
	var once sync.Once
	var outcomes map[string]compilerOutcome
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// Run selects the actual named subtests before Parallel releases them.
			// Thus -run/-skip filters still select individual cases, while the
			// complete selected list is frozen before the first compiler starts.
			selected = append(selected, item)
			t.Parallel()
			once.Do(func() { outcomes = run(ctx, selected) })
			outcome, exists := outcomes[item.name]
			if !exists {
				t.Fatal("compiler case was not checked")
			}
			if outcome.err != nil {
				t.Fatalf("compiler acceptance: %v\n%s", outcome.err, outcome.output)
			}
		})
	}
}

func runCompilerBatches(ctx context.Context, cases []compilerCase) map[string]compilerOutcome {
	results := make(map[string]compilerOutcome, len(cases))
	for start := 0; start < len(cases); start += compilerBatchSize {
		batch := cases[start:min(start+compilerBatchSize, len(cases))]
		if err := ctx.Err(); err != nil {
			for _, item := range cases[start:] {
				results[item.name] = compilerOutcome{err: err}
			}
			break
		}
		for name, outcome := range runCompilerBatch(ctx, batch) {
			results[name] = outcome
		}
	}
	return results
}

func failedCompilerBatch(cases []compilerCase, err error, output string) map[string]compilerOutcome {
	results := make(map[string]compilerOutcome, len(cases))
	for _, item := range cases {
		results[item.name] = compilerOutcome{output: output, err: err}
	}
	return results
}

// This writer bounds retained output and stops an unexpectedly noisy owned
// compiler process. It is used by one exec copy goroutine per output stream.
type compilerOutput struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (w *compilerOutput) Len() int       { return w.data.Len() }
func (w *compilerOutput) String() string { return w.data.String() }
func (w *compilerOutput) Bytes() []byte  { return w.data.Bytes() }

func (w *compilerOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := w.limit - w.Len()
	if n > room {
		p = p[:room]
		w.exceeded = true
		w.cancel()
	}
	_, _ = w.data.Write(p)
	return n, nil
}

func runCompilerBatch(parent context.Context, cases []compilerCase) map[string]compilerOutcome {
	ctx, cancel := context.WithTimeout(parent, compilerBatchTimeout)
	defer cancel()
	args := []string{"test", "-json", "-vet=off", "-run=^$"}
	for _, item := range cases {
		args = append(args, "./testdata/compilefail/"+item.name)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	configureCompilerCommand(cmd)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	output := &compilerOutput{limit: compilerBatchOutputBytes, cancel: cancel}
	stderr := &compilerOutput{limit: 64 << 10, cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, stderr
	err := cmd.Run()
	if output.exceeded || stderr.exceeded {
		return failedCompilerBatch(cases, errors.New("compiler output exceeded its bound"), "")
	}
	if ctx.Err() != nil {
		return failedCompilerBatch(cases, fmt.Errorf("compiler batch canceled or timed out: %w", ctx.Err()), "")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return failedCompilerBatch(cases, fmt.Errorf("expected compiler rejection, command returned %v", err), stderr.String())
	}
	if stderr.Len() != 0 {
		return failedCompilerBatch(cases, errors.New("compiler reported an unstructured invocation failure"), stderr.String())
	}
	results, err := parseCompilerBatch(output.Bytes(), cases)
	if err != nil {
		return failedCompilerBatch(cases, err, "")
	}
	return results
}

type compilerBuild struct {
	output strings.Builder
	failed bool
}
type compilerTest struct {
	failedBuild    string
	failed, passed bool
}

// Native Go emits build events by ImportPath and test events by Package.
// FailedBuild links the two; a dependency failure never proves the selected
// package's own invalid contract. The overall command exit alone proves nothing.
func parseCompilerBatch(data []byte, cases []compilerCase) (map[string]compilerOutcome, error) {
	if len(data) > compilerBatchOutputBytes {
		return nil, errors.New("compiler JSON exceeded its bound")
	}
	builds := make(map[string]*compilerBuild)
	tests := make(map[string]*compilerTest)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var event struct{ Action, ImportPath, Output, Package, FailedBuild, Test string }
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("invalid compiler JSON output: %w", err)
		}
		switch event.Action {
		case "build-output", "build-fail":
			if event.ImportPath == "" {
				return nil, errors.New("compiler build event omitted its package identity")
			}
			build := builds[event.ImportPath]
			if build == nil {
				build = &compilerBuild{}
				builds[event.ImportPath] = build
			}
			if event.Action == "build-output" {
				build.output.WriteString(event.Output)
			} else {
				if build.failed {
					return nil, errors.New("duplicate compiler build failure")
				}
				build.failed = true
			}
		case "start", "output", "fail", "pass", "skip":
			if event.Package == "" || event.Test != "" {
				return nil, errors.New("unexpected compiler test event")
			}
			test := tests[event.Package]
			if test == nil {
				test = &compilerTest{}
				tests[event.Package] = test
			}
			if event.Action == "fail" {
				if test.failed {
					return nil, errors.New("duplicate compiler test failure")
				}
				test.failed, test.failedBuild = true, event.FailedBuild
			}
			if event.Action == "pass" || event.Action == "skip" {
				test.passed = true
			}
		default:
			return nil, fmt.Errorf("unsupported compiler JSON action %q", event.Action)
		}
	}
	results := make(map[string]compilerOutcome, len(cases))
	for _, item := range cases {
		pkg := compilerPackagePrefix + item.name
		test := tests[pkg]
		if test == nil || !test.failed || test.passed {
			results[item.name] = compilerOutcome{err: errors.New("invalid contract compiled, was skipped or has no failure result")}
			continue
		}
		// The fixtures contain ordinary invalid.go files, not test variants.
		if test.failedBuild != pkg {
			results[item.name] = compilerOutcome{err: fmt.Errorf("contract did not fail in its own package: %s", test.failedBuild)}
			continue
		}
		build := builds[test.failedBuild]
		if build == nil || !build.failed {
			results[item.name] = compilerOutcome{err: errors.New("compiler did not report an attributed build failure")}
			continue
		}
		output := build.output.String()
		if !matchesCompilerDiagnostic(output, item) {
			results[item.name] = compilerOutcome{output: output, err: errors.New("package failed without its own expected source diagnostic")}
		} else {
			results[item.name] = compilerOutcome{}
		}
	}
	return results, nil
}

func matchesCompilerDiagnostic(output string, item compilerCase) bool {
	var block strings.Builder
	owned := false
	for _, line := range strings.Split(output, "\n") {
		if match := compilerDiagnosticLine.FindStringSubmatch(line); len(match) != 0 {
			if owned && strings.Contains(block.String(), item.diagnostic) {
				return true
			}
			block.Reset()
			file := strings.ReplaceAll(match[1], `\`, "/")
			owned = file == "invalid.go" || strings.HasSuffix("/"+file, "/testdata/compilefail/"+item.name+"/invalid.go")
			if owned {
				block.WriteString(match[2])
				block.WriteByte('\n')
			}
		} else if owned && (strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ")) {
			block.WriteString(line)
			block.WriteByte('\n')
		} else {
			if owned && strings.Contains(block.String(), item.diagnostic) {
				return true
			}
			owned = false
			block.Reset()
		}
	}
	return owned && strings.Contains(block.String(), item.diagnostic)
}
