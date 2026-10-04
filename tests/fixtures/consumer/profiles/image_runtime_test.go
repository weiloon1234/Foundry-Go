package profiles_test

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

// Each child discovers its library once, exactly like an application process.
// No installed libraries or process-wide loader state are changed by the test.
func TestImageRuntimeMissingLibrary(t *testing.T) {
	if os.Getenv("FOUNDRY_TEST_IMAGE_RUNTIME_CHILD") == "1" {
		checkMissingImageRuntime(t)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid-library")
	if err := os.WriteFile(invalid, []byte("not a shared library"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, path string }{
		{"missing", filepath.Join(t.TempDir(), "missing-library")},
		{"invalid", invalid},
		{"relative", "relative-library"},
	}
	if runtime.GOOS == "darwin" {
		// A loadable system library has none of the required libvips symbols.
		cases = append(cases, struct{ name, path string }{"wrong-library", "/usr/lib/libSystem.B.dylib"})
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestImageRuntimeMissingLibrary$", "-test.timeout=25s")
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(), "FOUNDRY_TEST_IMAGE_RUNTIME_CHILD=1", "FOUNDRY_VIPS_LIBRARY="+item.path)
			var output imageRuntimeLog
			command.Stdout, command.Stderr = &output, &output
			if err := command.Run(); err != nil {
				t.Fatalf("missing runtime child: %v\n%s", err, output.String())
			}
		})
	}
}

func checkMissingImageRuntime(t *testing.T) {
	t.Helper()
	if imaging.DefaultConfig().Backend != imaging.AutoBackend {
		t.Fatal("default no longer discovers native capabilities")
	}
	for _, backend := range []imaging.Backend{imaging.AutoBackend, imaging.PortableBackend} {
		settings := profiles.NativeImageSettings()
		settings.Image.Config.Backend = backend
		var output imageRuntimeLog
		app, err := application.New(settings, application.WithLogger(slog.New(slog.NewJSONHandler(&output, nil)))).Build(t.Context())
		if err != nil {
			t.Fatal("unavailable runtime prevented application build", err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		})
		if err := app.Start(t.Context()); err != nil {
			t.Fatal("unavailable runtime prevented startup", err)
		}
		engine, err := app.Resources().Image()
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(engine.NativeError(), imaging.ErrNativeUnavailable) || engine.Capabilities().Backend != imaging.PortableBackend {
			t.Fatal("unavailable runtime advertised native capabilities")
		}
		created, err := engine.Create(t.Context(), 8, 4, color.NRGBA{G: 160, A: 255}, imaging.NewPlan())
		if err != nil {
			t.Fatal("portable creation failed", err)
		}
		resized, err := engine.ProcessBytes(t.Context(), created.Bytes(), imaging.NewPlan().Resize(16, 8).Format(imaging.PNG))
		if err != nil || resized.Info().Width != 16 || resized.Info().Height != 8 {
			t.Fatal("portable resize failed", err)
		}
		if info, err := engine.Inspect(t.Context(), resized.Bytes()); err != nil || info.Format != imaging.PNG {
			t.Fatal("portable inspection failed", err)
		}
		for _, plan := range []imaging.Plan{
			imaging.NewPlan().Format(imaging.HEIF), imaging.NewPlan().Format(imaging.JPEGXL), imaging.NewPlan().Format(imaging.JPEG2000),
			imaging.NewPlan().ToSRGB(), imaging.NewPlan().Metadata(imaging.PreserveColorProfile),
			imaging.NewPlan().SmartFill(4, 4, false, imaging.CropAttention),
		} {
			if err := engine.ValidatePlan(plan); !errors.Is(err, imaging.ErrNativeUnavailable) {
				t.Fatal("native plan did not report unavailability", err)
			}
			if result, err := engine.ProcessBytes(t.Context(), created.Bytes(), plan); !errors.Is(err, imaging.ErrNativeUnavailable) || result.Size() != 0 {
				t.Fatal("native request did not fail without partial output", err)
			}
		}
		svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="4"/>`)
		if _, err := engine.Inspect(t.Context(), svg); !errors.Is(err, imaging.ErrNativeUnavailable) {
			t.Fatal("native inspection did not report unavailability", err)
		}
		if result, err := engine.ProcessBytes(t.Context(), svg, imaging.NewPlan().Format(imaging.PNG)); !errors.Is(err, imaging.ErrNativeUnavailable) || result.Size() != 0 {
			t.Fatal("native input did not report unavailability", err)
		}
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-engine.Done():
		default:
			t.Fatal("application did not close its portable engine")
		}
		warnings := strings.Count(output.String(), "native image features are unavailable")
		wantWarnings := 0
		if backend == imaging.AutoBackend {
			wantWarnings = 1
		}
		if warnings != wantWarnings {
			t.Fatal("startup warning count", warnings, wantWarnings)
		}
		if strings.Contains(output.String(), os.Getenv("FOUNDRY_VIPS_LIBRARY")) {
			t.Fatal("diagnostic exposed the configured library path")
		}
	}
	strict := profiles.NativeImageSettings()
	strict.Image.Config.Backend = imaging.LibvipsBackend
	if engine, err := imaging.New(strict.Image.Config); !errors.Is(err, imaging.ErrNativeUnavailable) || engine != nil {
		t.Fatal("required runtime silently fell back", err)
	}
	app, err := application.New(strict, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Build(t.Context())
	if app != nil {
		t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
		if err == nil {
			err = app.Start(t.Context())
		}
	}
	if err == nil {
		t.Fatal("required native application started without the runtime")
	}
}

// Lifecycle logging can use multiple goroutines. Also bound child diagnostics.
type imageRuntimeLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *imageRuntimeLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > 256<<10-b.data.Len() {
		return 0, io.ErrShortBuffer
	}
	return b.data.Write(p)
}

func (b *imageRuntimeLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
