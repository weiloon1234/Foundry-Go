package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	apppkg "github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type recordingTB struct {
	testing.TB
	messages []string
	errors   []error
	cleanups []func()
	fatal    bool
}

func (t *recordingTB) Helper() {}
func (t *recordingTB) Cleanup(f func()) {
	t.cleanups = append(t.cleanups, f)
	t.TB.Cleanup(f)
}
func (t *recordingTB) Error(args ...any) {
	t.messages = append(t.messages, fmt.Sprint(args...))
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			t.errors = append(t.errors, err)
		}
	}
}
func (t *recordingTB) Errorf(format string, args ...any) {
	t.messages = append(t.messages, fmt.Sprintf(format, args...))
}
func (t *recordingTB) Fatalf(format string, args ...any) {
	t.Errorf(format, args...)
	t.fatal = true
	panic("recorded fatal")
}

type stubKernel struct {
	run func(context.Context) error
}

func (s stubKernel) Run(ctx context.Context, _ foundation.KernelKind) error { return s.run(ctx) }
func (stubKernel) StopDelay() time.Duration                                 { return 0 }
func (stubKernel) ShutdownTimeout() time.Duration                           { return 0 }

func TestStopOnceAndRunResults(t *testing.T) {
	failure := errors.New("kernel failure")
	for _, result := range []error{nil, context.Canceled, errors.Join(context.Canceled, failure)} {
		t.Run(fmt.Sprintf("%v", result), func(t *testing.T) {
			recorded := &recordingTB{TB: t}
			entered := make(chan struct{})
			stop := runKernel(recorded, stubKernel{func(ctx context.Context) error {
				close(entered)
				<-ctx.Done()
				return result
			}}, foundation.Worker)
			<-entered
			stop()
			stop()
			recorded.cleanups[0]()
			if errors.Is(result, failure) {
				if len(recorded.messages) != 1 || !strings.Contains(recorded.messages[0], "kernel failure") {
					t.Fatal(recorded.messages)
				}
			} else if len(recorded.messages) != 0 {
				t.Fatal(recorded.messages)
			}
		})
	}
}

func TestStopReportsDrainTimeout(t *testing.T) {
	recorded := &recordingTB{TB: t}
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() { close(release); <-exited }()
	stop := runKernel(recorded, stubKernel{func(context.Context) error {
		close(entered)
		<-release
		close(exited)
		return nil
	}}, foundation.Worker)
	<-entered
	stop()
	recorded.cleanups[0]()
	if len(recorded.messages) != 1 || recorded.messages[0] != "worker kernel did not drain within 2s" {
		t.Fatal(recorded.messages)
	}
}

func buildHTTP(t *testing.T, providers ...foundation.Provider) *apppkg.App {
	t.Helper()
	settings := apppkg.DefaultSettings()
	settings.HTTP.Server.Address = "127.0.0.1:0"
	app, err := apppkg.New(settings, apppkg.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(providers...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRunHTTPReadyAndCleanup(t *testing.T) {
	app := buildHTTP(t)
	recorded := &recordingTB{TB: t}
	base := RunHTTP(recorded, app)
	response, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	recorded.cleanups[0]()
	recorded.cleanups[0]()
	if len(recorded.messages) != 0 || app.State() != foundation.Stopped {
		t.Fatal(recorded.messages, app.State())
	}
}

func TestRunHTTPReportsBootFailure(t *testing.T) {
	failure := errors.New("boot failed")
	app := buildHTTP(t, foundation.Module{Name: "failed", OnBoot: func(context.Context, *foundation.Runtime) error { return failure }})
	recorded := &recordingTB{TB: t}
	func() {
		defer func() {
			if p := recover(); p != "recorded fatal" {
				t.Fatalf("unexpected panic: %v", p)
			}
		}()
		RunHTTP(recorded, app)
	}()
	for _, cleanup := range recorded.cleanups {
		cleanup()
	}
	messages := strings.Join(recorded.messages, "\n")
	if !recorded.fatal || (len(recorded.errors) != 1 || !errors.Is(recorded.errors[0], failure)) || !strings.Contains(messages, "HTTP kernel is not ready") {
		t.Fatal(messages)
	}
}
