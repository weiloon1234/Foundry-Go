package consumer_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Exercise the consumer's module-selected CLI, including when this fixture is
// downloaded from a private candidate proxy without workspace replacements.
func TestModulePinnedFoundryTool(t *testing.T) {
	testkit.TrackExternalInputs(t)
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, goBinary, append([]string{"tool", "foundry"}, args...)...)
		configureCompilerCommand(command)
		command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
		stdout := &compilerOutput{limit: 64 << 10, cancel: cancel}
		stderr := &compilerOutput{limit: 64 << 10, cancel: cancel}
		command.Stdout, command.Stderr = stdout, stderr
		err := command.Run()
		if err != nil || stdout.exceeded || stderr.exceeded || ctx.Err() != nil {
			t.Fatalf("module-pinned CLI failed: %v\n%s\n%s", err, stdout.String(), stderr.String())
		}
		return stdout.Bytes()
	}
	var report struct {
		Checks []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
			Passed   bool   `json:"passed"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(run("doctor", "--go", goBinary, "--format", "json"), &report); err != nil {
		t.Fatal(err)
	}
	checks := make(map[string]bool)
	for _, check := range report.Checks {
		if check.Required && !check.Passed {
			t.Fatal("required prerequisite failed", check.Name)
		}
		checks[check.Name] = check.Passed
	}
	for _, name := range []string{"go", "module", "framework", "go-requirement"} {
		if !checks[name] {
			t.Fatal("missing successful prerequisite", name)
		}
	}
	run("generate", "--recursive", "--check", "--dir", "productionprofile")
}
