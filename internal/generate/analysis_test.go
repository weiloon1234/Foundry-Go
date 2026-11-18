package generate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAnalysisLoadsTestVariantsAndBodies(t *testing.T) {
	dir := fixture(t, "package sample\nfunc Value() int { return 1 }\n")
	for name, source := range map[string]string{
		"local_test.go":    "package sample\nfunc testOnly() int { return Value() }\n",
		"external_test.go": "package sample_test\nimport sample \"foundry.test/generator\"\nfunc external() int { return sample.Value() }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	packages, err := LoadAnalysis(t.Context(), dir, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, p := range packages {
		for identifier, object := range p.TypesInfo.Defs {
			if object != nil {
				found[identifier.Name] = true
			}
		}
	}
	for _, name := range []string{"Value", "testOnly", "external"} {
		if !found[name] {
			t.Errorf("analysis omitted %s", name)
		}
	}
}

func TestAnalysisCancellation(t *testing.T) {
	dir := fixture(t, "package sample\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := LoadAnalysis(ctx, dir, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	expired, cancelDeadline := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := LoadAnalysis(expired, dir, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	g := &packageGraph{root: dir}
	if err := g.goList(expired, []string{"list", "."}, func(listedPackage) {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestAnalysisDeadlineKillsGoCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses a POSIX shell")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf started > started\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, command := range []string{"go list", "go mod edit"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example/timeout\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// The deadline fires once the command has started, so a slow
			// process start cannot be mistaken for a missing kill.
			started := filepath.Join(dir, "started")
			ctx := &deadlineOnDemand{Context: t.Context(), done: make(chan struct{})}
			go func() {
				defer close(ctx.done)
				for limit := time.Now().Add(time.Minute); time.Now().Before(limit); time.Sleep(5 * time.Millisecond) {
					if _, err := os.Stat(started); err == nil {
						return
					}
				}
			}()
			var err error
			if command == "go list" {
				g := &packageGraph{root: dir}
				err = g.goList(ctx, []string{"list", "."}, func(listedPackage) {})
			} else {
				_, err = loadModuleScope(ctx, dir)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("killed command must preserve deadline: %v", err)
			}
			if _, err := os.Stat(started); err != nil {
				t.Fatalf("command never started: %v", err)
			}
		})
	}
}

// deadlineOnDemand reports an expired deadline once the test closes done.
type deadlineOnDemand struct {
	context.Context
	done chan struct{}
}

func (c *deadlineOnDemand) Done() <-chan struct{} { return c.done }

func (c *deadlineOnDemand) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
