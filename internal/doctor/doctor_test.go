package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
			if len(calls) < 2 || len(calls) > 4 {
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
