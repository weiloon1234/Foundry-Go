package cli_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

type copyArguments struct {
	Force  bool
	From   string
	Copies int
}

func TestPositionalArgumentsBindIntoTypedStruct(t *testing.T) {
	decode := cli.FlagsWithArgs(func(flags *flag.FlagSet, args *copyArguments) {
		flags.BoolVar(&args.Force, "force", false, "overwrite")
	}, cli.ExactArgs(2, func(args *copyArguments, values []string) error {
		copies, err := strconv.Atoi(values[1])
		args.From, args.Copies = values[0], copies
		return err
	}), nil)
	args, err := decode([]string{"--force", "source.txt", "3"}, io.Discard)
	if err != nil || !args.Force || args.From != "source.txt" || args.Copies != 3 {
		t.Fatal("positional binding failed", args, err)
	}
	for _, input := range [][]string{{"only-one"}, {"a", "b", "c"}, {"a", "not-a-number"}} {
		if _, err := decode(input, io.Discard); cli.Status(err) != cli.InvalidUsage {
			t.Fatal("invalid positional input was not a usage error", input, err)
		}
	}
	if args, err := decode([]string{"--", "--force", "7"}, io.Discard); err != nil || args.Force || args.From != "--force" {
		t.Fatal("terminator did not end flag parsing", args, err)
	}
	flagsOnly := cli.Flags(func(*flag.FlagSet, *copyArguments) {}, nil)
	if _, err := flagsOnly([]string{"extra"}, io.Discard); cli.Status(err) != cli.InvalidUsage {
		t.Fatal("flags-only decoder accepted a positional argument")
	}
}

func TestConfirmRequiresExplicitAnswerOrForce(t *testing.T) {
	var prompt strings.Builder
	ask := func(input string, force bool) error {
		return cli.Confirm(t.Context(), cli.Streams{In: strings.NewReader(input), Out: io.Discard, Err: &prompt}, "Drop the cache?", force)
	}
	if err := ask("yes\nrest", false); err != nil || !strings.Contains(prompt.String(), "Drop the cache? [y/N]") {
		t.Fatal("confirmation failed", err, prompt.String())
	}
	for _, answer := range []string{"", "no\n", "yess\n", "\n"} {
		if err := ask(answer, false); !errors.Is(err, cli.ErrNotConfirmed) || cli.Status(err) != cli.Failure {
			t.Fatal("unconfirmed answer proceeded", answer, err)
		}
	}
	if err := ask("", true); err != nil {
		t.Fatal("forced confirmation prompted or failed", err)
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "answers"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	prompt.Reset()
	if err := cli.Confirm(t.Context(), cli.Streams{In: file, Out: io.Discard, Err: &prompt}, "Drop?", false); !errors.Is(err, cli.ErrNotConfirmed) || prompt.Len() != 0 {
		t.Fatal("non-interactive input was treated as confirmation", err)
	}
	if err := cli.ConfirmInProduction(t.Context(), cli.Streams{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}, false, "Drop?", false); err != nil {
		t.Fatal("non-production confirmation prompted", err)
	}
	if err := cli.ConfirmInProduction(t.Context(), cli.Streams{In: strings.NewReader("n\n"), Out: io.Discard, Err: io.Discard}, true, "Drop?", false); !errors.Is(err, cli.ErrNotConfirmed) {
		t.Fatal("production confirmation proceeded without consent", err)
	}
}

func TestWriteTableAlignsAndNeutralizesControlCharacters(t *testing.T) {
	var output strings.Builder
	if err := cli.WriteTable(&output, []string{"Name", "Driver"}, [][]string{{"default", "postgres"}, {"reports\n\tforged", "redis"}}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 3 || strings.Index(lines[0], "Driver") != strings.Index(lines[1], "postgres") || strings.Contains(output.String(), "\t") {
		t.Fatal("table was not aligned or retained injected layout", output.String())
	}
	if err := cli.WriteTable(&output, []string{"Name"}, [][]string{{"a", "b"}}); err == nil {
		t.Fatal("mismatched row accepted")
	}
}

func TestMaintenanceAdmissionAllowsDeclaredOrFlaggedCommands(t *testing.T) {
	run := func(allowed bool) (*cli.Registry, *int) {
		calls := new(int)
		definition := command()
		if allowed {
			definition = definition.AllowDuringMaintenance()
		}
		declaration, err := definition.Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
			return func(context.Context, arguments, cli.Streams) error { *calls++; return nil }, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		registry, err := cli.New(declaration)
		if err != nil {
			t.Fatal(err)
		}
		return registry, calls
	}
	gate := &maintenance.Gate{}
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	ctx := maintenance.WithContext(t.Context(), gate)
	invoke := func(registry *cli.Registry, args ...string) error {
		invocation, err := registry.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return invocation.Run(ctx, nil, streams(io.Discard))
	}
	ordinary, ordinaryCalls := run(false)
	if err := invoke(ordinary, "greet", "--name", "Ada"); !errors.Is(err, maintenance.ErrMaintenance) || *ordinaryCalls != 0 {
		t.Fatal("ordinary command ran during maintenance", err)
	}
	if err := invoke(ordinary, cli.DuringMaintenance, "greet", "--name", "Ada"); err != nil || *ordinaryCalls != 1 {
		t.Fatal("explicit maintenance flag did not admit the command", err)
	}
	operational, operationalCalls := run(true)
	if description := operational.Describe(); !description[0].Maintenance {
		t.Fatal("maintenance permission missing from command metadata")
	}
	if err := invoke(operational, "greet", "--name", "Ada"); err != nil || *operationalCalls != 1 {
		t.Fatal("declared operational command was rejected", err)
	}
	gate.Drain()
	if err := invoke(operational, "greet", "--name", "Ada"); !errors.Is(err, maintenance.ErrDraining) {
		t.Fatal("draining admitted a maintenance command", err)
	}
	if err := invoke(ordinary, cli.DuringMaintenance, "greet", "--name", "Ada"); !errors.Is(err, maintenance.ErrDraining) {
		t.Fatal("draining admitted a flagged command", err)
	}
	if _, err := ordinary.Parse([]string{cli.DuringMaintenance}, io.Discard); cli.Status(err) != cli.InvalidUsage {
		t.Fatal("maintenance flag without a command was accepted")
	}
}
