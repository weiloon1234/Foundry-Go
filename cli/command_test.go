package cli_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type arguments struct {
	Name  string
	Count int
	Loud  bool
}

func command() cli.Command[arguments] {
	return cli.Define("greet", "Greet a named recipient", cli.Flags(func(flags *flag.FlagSet, args *arguments) {
		flags.StringVar(&args.Name, "name", "", "recipient name")
		flags.IntVar(&args.Count, "count", 1, "number of greetings")
		flags.BoolVar(&args.Loud, "loud", false, "uppercase greeting")
	}, func(args arguments) error {
		if args.Name == "" || args.Count < 1 {
			return cli.Usage("--name and a positive --count are required")
		}
		return nil
	}))
}
func streams(output io.Writer) cli.Streams {
	return cli.Streams{In: strings.NewReader(""), Out: output, Err: output}
}

func TestHelpAndInvalidInputNeverConstructServices(t *testing.T) {
	var constructed atomic.Int32
	declaration, err := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
		constructed.Add(1)
		return func(context.Context, arguments, cli.Streams) error { return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--help"}, {"greet", "--help"}, {"unknown"}, {"greet", "--count", "0"}, {"greet", "--name", "Ada", "extra"}, {"greet", "--count", "wrong"}} {
		var output strings.Builder
		_, err := registry.Parse(args, &output)
		want := cli.InvalidUsage
		if slices.Contains(args, "--help") {
			want = cli.Success
			if output.Len() == 0 {
				t.Fatal("help produced no output")
			}
		}
		if cli.Status(err) != want {
			t.Fatal("wrong argument status", args, cli.Status(err), err)
		}
	}
	if constructed.Load() != 0 {
		t.Fatal("parsing constructed a service")
	}
	if _, err := cli.New(declaration, declaration); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := registry.Parse([]string{"greet", "--name", strings.Repeat("x", cli.MaxArgumentBytes+1)}, io.Discard); cli.Status(err) != cli.InvalidUsage {
		t.Fatal(err)
	}
	if _, err := registry.Parse(make([]string, cli.MaxArguments+1), io.Discard); cli.Status(err) != cli.InvalidUsage {
		t.Fatal(err)
	}
	descriptions := registry.Describe()
	descriptions[0].Name = "changed"
	if registry.Describe()[0].Name != "greet" {
		t.Fatal("inspection changed registry")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestHelpWriteFailureAndExitStatuses(t *testing.T) {
	d, _ := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) { return nil, nil })
	r, _ := cli.New(d)
	for _, args := range [][]string{{"--help"}, {"greet", "--help"}} {
		if _, err := r.Parse(args, brokenWriter{}); cli.Status(err) != cli.Failure || !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal("help write failure reported success", err)
		}
	}
	for _, item := range []struct {
		err  error
		want cli.ExitCode
	}{{nil, cli.Success}, {flag.ErrHelp, cli.Success}, {errors.Join(flag.ErrHelp, io.ErrClosedPipe), cli.Failure}, {context.Canceled, cli.Interrupted}, {context.DeadlineExceeded, cli.TimedOut}, {cli.Usage("bad input"), cli.InvalidUsage}, {errors.New("execution failed"), cli.Failure}} {
		if got := cli.Status(item.err); got != item.want {
			t.Fatal(got, item.want)
		}
	}
	var out strings.Builder
	if code := cli.Report(&out, cli.Usage("explain the missing name")); code != cli.InvalidUsage || !strings.Contains(out.String(), "explain the missing name") {
		t.Fatal("usage diagnostic lost", code)
	}
	if code := cli.Report(brokenWriter{}, context.Canceled); code != cli.Failure {
		t.Fatal("failed reporting was ignored")
	}
}

func TestInvocationRunsOnceAndContainsCallbackFailures(t *testing.T) {
	for _, mode := range []string{"ok", "panic", "goexit", "constructor", "nil"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			d, _ := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
				if mode == "constructor" {
					panic("private constructor")
				}
				if mode == "nil" {
					return nil, nil
				}
				return func(_ context.Context, args arguments, _ cli.Streams) error {
					calls.Add(1)
					if args.Name != "Ada" || args.Count != 2 || !args.Loud {
						return errors.New("typed arguments changed")
					}
					switch mode {
					case "panic":
						panic("private payload")
					case "goexit":
						runtime.Goexit()
					}
					return nil
				}, nil
			})
			r, _ := cli.New(d)
			invocation, err := r.Parse([]string{"greet", "--name", "Ada", "--count", "2", "--loud"}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			err = invocation.Run(t.Context(), nil, streams(io.Discard))
			if (err == nil) != (mode == "ok") {
				t.Fatal(mode, err)
			}
			if err := invocation.Run(t.Context(), nil, streams(io.Discard)); !errors.Is(err, fault.Closed) {
				t.Fatal("invocation reran", err)
			}
			if calls.Load() > 1 {
				t.Fatal("handler reran")
			}
		})
	}
	var calls atomic.Int32
	d, _ := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
		return func(context.Context, arguments, cli.Streams) error { calls.Add(1); return nil }, nil
	})
	r, _ := cli.New(d)
	i, _ := r.Parse([]string{"greet", "--name", "Ada"}, io.Discard)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			err := i.Run(t.Context(), nil, streams(io.Discard))
			if err != nil && !errors.Is(err, fault.Closed) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("copied invocation lost execution claim")
	}
}

type service struct{ ready bool }

func TestModuleUsesSharedBootstrapAndCleansUpAfterCommand(t *testing.T) {
	dependency := foundation.NewKey[*service]("test.service")
	registryKey := foundation.NewKey[*cli.Registry]("test.commands")
	state := &service{}
	var order []string
	provider := foundation.Module{Name: "infra", OnRegister: func(r *foundation.Registrar) error { return foundation.Provide(r, dependency, state) }, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		state.ready = true
		order = append(order, "boot")
		return r.OnShutdown("resource", func(context.Context) error { state.ready = false; order = append(order, "close"); return nil })
	}}
	declaration, _ := command().Declare(func(resolver foundation.Resolver) (cli.Handler[arguments], error) {
		s, err := foundation.Resolve(resolver, dependency)
		if err != nil {
			return nil, err
		}
		if !s.ready {
			return nil, errors.New("command constructed before dependency boot")
		}
		order = append(order, "construct")
		return func(_ context.Context, a arguments, out cli.Streams) error {
			if !s.ready {
				return errors.New("closed dependency")
			}
			order = append(order, "run")
			_, err := io.WriteString(out.Out, a.Name)
			return err
		}, nil
	})
	r, _ := cli.New(declaration)
	var output strings.Builder
	invocation, err := r.Parse([]string{"greet", "--name", "Ada"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	builder := foundry.New().Register(provider, cli.Module("commands", registryKey, r, invocation, streams(&output), provider.Name))
	if _, err := builder.Inspect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatal("declaration inspection started services")
	}
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(t.Context(), foundation.CLI); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"boot", "construct", "run", "close"}) || output.String() != "Ada" || app.State() != foundation.Stopped {
		t.Fatal("CLI lifecycle changed", order, app.State())
	}
}

func TestCommandCancellationAndPartialBootstrapReleaseResources(t *testing.T) {
	for _, mode := range []string{"cancel", "handler-failure", "boot-failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var acquired, closed, constructed atomic.Bool
			failure := errors.New("expected execution failure")
			provider := foundation.Module{Name: "resource", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
				acquired.Store(true)
				if err := r.OnShutdown("resource", func(context.Context) error { closed.Store(true); return nil }); err != nil {
					return err
				}
				if mode == "boot-failure" {
					return failure
				}
				return nil
			}}
			declaration, err := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
				constructed.Store(true)
				return func(ctx context.Context, _ arguments, _ cli.Streams) error {
					if mode == "cancel" {
						cancel()
						<-ctx.Done()
						return ctx.Err()
					}
					return failure
				}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			registry, err := cli.New(declaration)
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := registry.Parse([]string{"greet", "--name", "Ada"}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			builder := foundry.New().Register(provider, cli.Module("cli", foundation.NewKey[*cli.Registry]("commands"), registry, invocation, streams(io.Discard), provider.Name))
			app, err := builder.Build(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = app.Run(ctx, foundation.CLI)
			want := failure
			if mode == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || !acquired.Load() || !closed.Load() {
				t.Fatal("failed/canceled command lost cleanup or cause", err)
			}
			if mode == "boot-failure" && constructed.Load() {
				t.Fatal("failed bootstrap constructed handler")
			}
		})
	}
}

type hostileError struct{ exit bool }

func (e hostileError) Error() string {
	if e.exit {
		runtime.Goexit()
	}
	panic("private diagnostic")
}
func (e hostileError) Is(error) bool {
	if e.exit {
		runtime.Goexit()
	}
	panic("private classification")
}
func TestCustomErrorMethodsCannotEscapeCLIReporting(t *testing.T) {
	for _, exit := range []bool{false, true} {
		if cli.Status(hostileError{exit}) != cli.Failure || cli.Report(io.Discard, hostileError{exit}) != cli.Failure {
			t.Fatal("hostile error escaped reporting")
		}
	}
}
func TestCanceledInvocationDoesNotConstructOrConsumeClaim(t *testing.T) {
	var count atomic.Int32
	declaration, err := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
		count.Add(1)
		return func(context.Context, arguments, cli.Streams) error { return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := registry.Parse([]string{"greet", "--name", "Ada"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := invocation.Run(ctx, nil, streams(io.Discard)); !errors.Is(err, context.Canceled) || count.Load() != 0 {
		t.Fatal("canceled invocation constructed handler", err)
	}
	if err := invocation.Run(t.Context(), nil, streams(io.Discard)); err != nil || count.Load() != 1 {
		t.Fatal("canceled preflight consumed invocation", err)
	}
}
