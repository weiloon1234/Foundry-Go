package consumer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func compilerEvents(t *testing.T, events ...map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

func rejectedCompilerEvents(name, diagnostic string) []map[string]string {
	pkg := compilerPackagePrefix + name
	return []map[string]string{
		{"Action": "build-output", "ImportPath": pkg, "Output": "# " + pkg + "\n"},
		{"Action": "build-output", "ImportPath": pkg, "Output": "testdata/compilefail/" + name + "/invalid.go:7:2: " + diagnostic + "\n"},
		{"Action": "build-fail", "ImportPath": pkg},
		{"Action": "start", "Package": pkg},
		{"Action": "output", "Package": pkg, "Output": "FAIL [build failed]\n"},
		{"Action": "fail", "Package": pkg, "FailedBuild": pkg},
	}
}

func TestCompilerBatchAttributesInterleavedBuildAndTestStreams(t *testing.T) {
	cases := []compilerCase{{"alpha", "missing method Alpha"}, {"beta", "cannot use Beta"}}
	a, b := rejectedCompilerEvents(cases[0].name, cases[0].diagnostic), rejectedCompilerEvents(cases[1].name, cases[1].diagnostic)
	var interleaved []map[string]string
	for i := range a {
		interleaved = append(interleaved, a[i], b[i])
	}
	results, err := parseCompilerBatch(compilerEvents(t, interleaved...), cases)
	if err != nil || len(results) != 2 {
		t.Fatal(results, err)
	}
	for _, item := range cases {
		if results[item.name].err != nil {
			t.Fatal(item.name, results[item.name])
		}
	}
}

func TestCompilerBatchRejectsWrongFailureEvidence(t *testing.T) {
	item := compilerCase{"alpha", "missing method Alpha"}
	for _, mutate := range []func([]map[string]string) []map[string]string{
		func(events []map[string]string) []map[string]string { return events[:len(events)-1] },
		func(events []map[string]string) []map[string]string {
			events[5]["FailedBuild"] = "dependency.test/broken"
			return events
		},
		func(events []map[string]string) []map[string]string {
			events[1]["Output"] = "testdata/compilefail/beta/invalid.go:7:2: missing method Alpha\n"
			return events
		},
		func(events []map[string]string) []map[string]string {
			events[1]["Output"] = "testdata/compilefail/alpha/invalid.go:7:2: unrelated failure\n"
			return events
		},
		func(events []map[string]string) []map[string]string {
			events[2]["Action"] = "build-output"
			return events
		},
		func(events []map[string]string) []map[string]string { events[5]["Action"] = "pass"; return events },
		func(events []map[string]string) []map[string]string { events[5]["Action"] = "skip"; return events },
		func(events []map[string]string) []map[string]string {
			events[1]["Output"] = "missing method Alpha\n"
			return events
		},
	} {
		events := mutate(rejectedCompilerEvents(item.name, item.diagnostic))
		results, err := parseCompilerBatch(compilerEvents(t, events...), []compilerCase{item})
		if err == nil && results[item.name].err == nil {
			t.Fatal("incorrect failure accepted", events)
		}
	}
	// One rejected package does not prove another selected package was compiled.
	results, err := parseCompilerBatch(compilerEvents(t, rejectedCompilerEvents(item.name, item.diagnostic)...), []compilerCase{item, {"beta", "missing method Beta"}})
	if err != nil || results["alpha"].err != nil || results["beta"].err == nil {
		t.Fatal("batch completeness", results, err)
	}
}

func TestCompilerDiagnosticContinuationBelongsToItsSource(t *testing.T) {
	item := compilerCase{"alpha", "missing method Alpha"}
	for _, output := range []string{
		"testdata/compilefail/alpha/invalid.go:2:3: incompatible value:\n\tmissing method Alpha\n",
		"invalid.go:2: incompatible value:\n  missing method Alpha\n",
		"C:\\workspace\\testdata\\compilefail\\alpha\\invalid.go:2:3: missing method Alpha\n",
	} {
		if !matchesCompilerDiagnostic(output, item) {
			t.Fatal("own diagnostic rejected", output)
		}
	}
	for _, output := range []string{
		"testdata/compilefail/alpha/invalid.go:2:3: unrelated\ntestdata/compilefail/beta/invalid.go:2:3: missing method Alpha\n",
		"testdata/compilefail/alpha/invalid.go:x:y: missing method Alpha\n",
		"testdata/compilefail/alpha/invalid.go:2:3: unrelated\n# missing method Alpha\n",
	} {
		if matchesCompilerDiagnostic(output, item) {
			t.Fatal("foreign diagnostic accepted", output)
		}
	}
}

func TestCompilerBatchRejectsMalformedAndDuplicateProtocol(t *testing.T) {
	item := compilerCase{"alpha", "missing method Alpha"}
	for _, data := range [][]byte{
		[]byte("go: cannot resolve module\n"),
		[]byte(`{"Action":"build-fail"}`),
		[]byte(`{"Action":"unexpected"}`),
		[]byte(`{"Action":"pass","Package":"alpha","Test":"UnexpectedTest"}`),
		[]byte(`{"Action":"fail","Package":"alpha"} {"Action":"fail","Package":"alpha"}`),
		[]byte(`{"Action":"build-fail","ImportPath":"alpha"} {"Action":"build-fail","ImportPath":"alpha"}`),
		bytes.Repeat([]byte(" "), compilerBatchOutputBytes+1),
	} {
		if _, err := parseCompilerBatch(data, []compilerCase{item}); err == nil {
			t.Fatal("invalid compiler protocol accepted")
		}
	}
}

func TestCompilerOutputBoundCancelsAndKeepsOnlyItsBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := &compilerOutput{limit: 8, cancel: cancel}
	if n, err := output.Write([]byte("1234567890")); n != 10 || err != nil || output.String() != "12345678" || !output.exceeded || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("compiler output bound", n, err, output.String())
	}
	copyContext, copyCancel := context.WithCancel(t.Context())
	defer copyCancel()
	copied := &compilerOutput{limit: 8, cancel: copyCancel}
	// exec copies from pipe readers. A promoted bytes.Buffer.ReadFrom must not
	// bypass the bounded Write method through io.Copy's fast path.
	_, err := io.Copy(copied, struct{ io.Reader }{strings.NewReader("1234567890")})
	if err != nil || copied.Len() != 8 || !copied.exceeded || copyContext.Err() == nil {
		t.Fatal("copy bypassed output bounds", err, copied.Len())
	}
}

func TestCompilerNamedSelectionPrecedesBatchExecution(t *testing.T) {
	for _, test := range []struct{ filter, want string }{{"beta", "beta"}, {"(alpha|gamma)", "alpha,gamma"}} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompilerSelectionHelper$/^"+test.filter+"$", "--", "compiler-selection")
		configureCompilerCommand(cmd)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil || strings.Count(string(output), "selected:") != 1 || !strings.Contains(string(output), "selected:"+test.want+"\n") {
			t.Fatalf("named selection did not drive one exact batch: %v\n%s", err, output)
		}
	}
}

func TestCompilerSelectionHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-1] != "compiler-selection" || os.Args[len(os.Args)-2] != "--" {
		return
	}
	cases := []compilerCase{{"alpha", "expected"}, {"beta", "expected"}, {"gamma", "expected"}}
	checkCompilerCases(t, cases, func(ctx context.Context, selected []compilerCase) map[string]compilerOutcome {
		names := make([]string, len(selected))
		results := make(map[string]compilerOutcome)
		for i, item := range selected {
			names[i] = item.name
			results[item.name] = compilerOutcome{err: ctx.Err()}
		}
		fmt.Println("selected:" + strings.Join(names, ","))
		return results
	})
}
