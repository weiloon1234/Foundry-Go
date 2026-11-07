package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func TestDoctorUsesOfflineModuleMetadataAndOptionalTools(t *testing.T) {
	for _, mode := range []string{"ok", "missing-gopls", "required-gopls", "old-go", "missing-framework", "bad-module", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			var calls []string
			options := Options{Timeout: time.Second, RequireGopls: mode == "required-gopls"}
			report, err := inspect(t.Context(), options, func(_ context.Context, _ Options, tool string, args ...string) ([]byte, error) {
				command := strings.Join(append([]string{tool}, args...), " ")
				calls = append(calls, command)
				switch command {
				case "go env GOVERSION":
					if mode == "old-go" {
						return []byte("go1.20.1"), nil
					}
					return []byte("go1.27.1"), nil
				case "go list -m -json":
					if mode == "bad-module" {
						return []byte("not-json private-env-value"), nil
					}
					return json.Marshal(moduleInfo{Path: "consumer.example/app", Main: true, GoVersion: "1.27.1"})
				case "go list -m -json " + frameworkinfo.ModulePath:
					if mode == "missing-framework" {
						return nil, errors.New("private credential from tool")
					}
					module := moduleInfo{Path: frameworkinfo.ModulePath, GoVersion: "1.27.1"}
					if mode == "replacement" {
						module.Replace = &moduleInfo{Path: "private-workspace", GoVersion: "1.28.0"}
					}
					return json.Marshal(module)
				case "gopls version":
					if mode == "missing-gopls" || mode == "required-gopls" {
						return nil, errors.New("missing executable")
					}
					return []byte("golang.org/x/tools/gopls v0.23.0"), nil
				default:
					t.Fatal("doctor tried an unexpected operation", command)
					return nil, nil
				}
			})
			wantSuccess := mode == "ok" || mode == "missing-gopls"
			if (err == nil) != wantSuccess {
				t.Fatal("incorrect required check result", mode, err)
			}
			if len(calls) < 2 || len(calls) > 5 {
				t.Fatal("unbounded doctor probes", calls)
			}
			data, _ := json.Marshal(report)
			if strings.Contains(string(data), "private") {
				t.Fatal("doctor echoed private tool output")
			}
		})
	}
}

func TestDoctorEnvironmentAndOutputAreBounded(t *testing.T) {
	environment := offlineEnvironment([]string{"PATH=/tools", "GOTOOLCHAIN=auto", "GOFLAGS=-mod=mod", "GONOPROXY=*", "GOPRIVATE=*", "GOPROXY=https://private.invalid", "GOENV=/private/env"})
	for _, required := range []string{"PATH=/tools", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOPRIVATE=", "GONOPROXY=none", "GOVCS=*:off", "GOENV=off"} {
		found := 0
		key, _, _ := strings.Cut(required, "=")
		for _, entry := range environment {
			if strings.HasPrefix(entry, key+"=") {
				found++
				if entry != required {
					t.Fatal("unsafe inherited tool setting", key)
				}
			}
		}
		if found != 1 {
			t.Fatal("missing/repeated offline setting", key)
		}
	}
	canceled := false
	buffer := &output{cancel: func() { canceled = true }}
	if _, err := io.Copy(buffer, strings.NewReader(strings.Repeat("x", maxOutputBytes+1))); err == nil || !canceled || buffer.data.Len() > maxOutputBytes {
		t.Fatal("tool output bypassed its limit")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := inspect(ctx, Options{Timeout: time.Second}, func(context.Context, Options, string, ...string) ([]byte, error) {
		t.Fatal("canceled doctor invoked tool")
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDoctorDeployToolIsOptional(t *testing.T) {
	for _, mode := range []string{"success", "python-failure", "no-tool"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			entry := filepath.Join(dir, frameworkinfo.DeployEntry)
			if mode != "no-tool" {
				if err := os.MkdirAll(filepath.Dir(entry), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(entry, nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			pythonCalls := 0
			options := Options{Timeout: time.Second}
			if mode == "python-failure" {
				options.Python = "custom-python"
			}
			report, err := inspect(t.Context(), options, func(_ context.Context, got Options, tool string, args ...string) ([]byte, error) {
				if tool == got.Python {
					pythonCalls++
					wantPython := "python3"
					if mode == "python-failure" {
						wantPython = "custom-python"
					}
					if tool != wantPython || !reflect.DeepEqual(args, []string{"-B", entry, "help"}) {
						t.Fatalf("unexpected Python probe: %s %q", tool, args)
					}
					if mode == "python-failure" {
						return []byte("private output"), errors.New("private error")
					}
					return []byte("help"), nil
				}
				switch strings.Join(append([]string{tool}, args...), " ") {
				case "go env GOVERSION":
					return []byte("go1.27.1"), nil
				case "go list -m -json":
					return json.Marshal(moduleInfo{Path: "consumer.example/app", Main: true, GoVersion: "1.27.1"})
				case "go list -m -json " + frameworkinfo.ModulePath:
					return json.Marshal(moduleInfo{Path: frameworkinfo.ModulePath, GoVersion: "1.27.1", Dir: dir})
				case "gopls version":
					return []byte("gopls v0.23.0"), nil
				default:
					t.Fatalf("unexpected probe: %s %q", tool, args)
					return nil, nil
				}
			})
			if err != nil {
				t.Fatal("optional deploy check failed doctor", err)
			}
			found := false
			for _, check := range report.Checks {
				if check.Name == "deploy-tool" {
					found = true
					if check.Required || check.Passed != (mode == "success") {
						t.Fatalf("incorrect optional deploy check: %+v", check)
					}
				}
			}
			if !found {
				t.Fatal("missing deploy-tool check")
			}
			wantCalls := 1
			if mode == "no-tool" {
				wantCalls = 0
			}
			if pythonCalls != wantCalls {
				t.Fatalf("Python probes = %d, want %d", pythonCalls, wantCalls)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private") {
				t.Fatal("doctor echoed private Python output")
			}
		})
	}
}

func TestSelectedFramework(t *testing.T) {
	for _, mode := range []string{"success", "replacement", "unreadable", "invalid-json", "module-error", "wrong-path", "empty-dir"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			framework, err := selectedFramework(t.Context(), Options{Timeout: time.Second}, func(_ context.Context, _ Options, tool string, args ...string) ([]byte, error) {
				if tool != "go" || !reflect.DeepEqual(args, []string{"list", "-m", "-json", frameworkinfo.ModulePath}) {
					t.Fatalf("unexpected framework probe: %s %q", tool, args)
				}
				module := moduleInfo{Path: frameworkinfo.ModulePath, Version: "v0.5.3", Dir: dir}
				switch mode {
				case "replacement":
					module.Replace = &moduleInfo{Path: "local-framework"}
				case "unreadable":
					return []byte("private output"), errors.New("private error")
				case "invalid-json":
					return []byte("private invalid JSON"), nil
				case "module-error":
					module.Error = &struct{ Err string }{Err: "private module error"}
				case "wrong-path":
					module.Path = "private module path"
				case "empty-dir":
					module.Dir = ""
				}
				return json.Marshal(module)
			})
			if mode == "success" || mode == "replacement" {
				want := frameworkinfo.Selection{Path: frameworkinfo.ModulePath, Version: "v0.5.3"}
				if mode == "replacement" {
					want = frameworkinfo.Selection{Path: "local-framework", Local: true}
				}
				if err != nil || framework.Dir != dir || framework.Selection != want {
					t.Fatalf("selected framework = %+v, %v", framework, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "go mod download "+frameworkinfo.ModulePath) {
				t.Fatal("missing module download guidance", err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("framework selection echoed private output")
			}
		})
	}
}
