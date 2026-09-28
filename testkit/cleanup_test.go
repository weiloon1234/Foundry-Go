package testkit_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

// A failing child test proves Cleanup runs after Fatal/Goexit without marking
// the parent suite failed. The marker is written only after actual app shutdown.
func TestOwnedApplicationCleanupAfterFatalAndPartialBoot(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fatal", "partial"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "closed")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestOwnedCleanupChild$")
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(), "FOUNDRY_CLEANUP_TEST_MODE="+mode, "FOUNDRY_CLEANUP_TEST_MARKER="+marker)
			output, err := command.CombinedOutput()
			if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "FAIL") {
				t.Fatal("cleanup child did not exercise a failing test", err)
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "closed" {
				t.Fatal("failed test did not release owned application resources", err)
			}
		})
	}
}
func TestOwnedCleanupChild(t *testing.T) {
	mode := os.Getenv("FOUNDRY_CLEANUP_TEST_MODE")
	if mode == "" {
		return
	}
	var closed atomic.Bool
	t.Cleanup(func() {
		if !closed.Load() {
			t.Error("application resource remained open")
			return
		}
		if err := os.WriteFile(os.Getenv("FOUNDRY_CLEANUP_TEST_MARKER"), []byte("closed"), 0600); err != nil {
			t.Error(err)
		}
	})
	provider := foundation.Module{Name: "owned", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("resource", func(context.Context) error { closed.Store(true); return nil }); err != nil {
			return err
		}
		if mode == "partial" {
			return fmt.Errorf("expected partial boot failure")
		}
		return nil
	}}
	testkit.Start(t, foundry.New().Register(provider))
	t.Fatal("expected test failure after successful startup")
}
