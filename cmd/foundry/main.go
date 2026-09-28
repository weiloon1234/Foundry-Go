// Command foundry provides framework development tools in a consumer workspace.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/agent"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	code := cli.Report(os.Stderr, run(ctx, os.Args[1:], os.Stdout, os.Stderr))
	cancel()
	if code != cli.Success {
		os.Exit(int(code))
	}
}
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	type arguments struct{ values []string }
	var declarations []cli.Declaration
	for _, entry := range []struct {
		name    cli.Name
		summary string
		run     func(context.Context, []string, io.Writer, io.Writer) error
	}{
		{"generate", "Generate or check typed consumer declarations", runGenerate},
		{"agent", "Inspect completion, hover and definition with gopls", runAgent},
		{"make", "Scaffold a model, DTO, job, command, migration or seeder", runMake},
		{"contracts", "Generate client contracts from an exported manifest", runContracts},
		{"doctor", "Inspect local framework/tool prerequisites without starting services", runDoctor},
	} {
		command := cli.Define(entry.name, entry.summary, func(args []string, _ io.Writer) (arguments, error) { return arguments{values: args}, nil })
		declaration, err := command.Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
			return func(ctx context.Context, args arguments, streams cli.Streams) error {
				return entry.run(ctx, args.values, streams.Out, streams.Err)
			}, nil
		})
		if err != nil {
			return err
		}
		declarations = append(declarations, declaration)
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		return err
	}
	invocation, err := registry.Parse(args, stdout)
	if err != nil {
		return err
	}
	return invocation.Run(ctx, nil, cli.Streams{In: os.Stdin, Out: stdout, Err: stderr})
}

func runGenerate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "existing consumer Go package directory")
	check := flags.Bool("check", false, "fail on stale output without rewriting files")
	recursive := flags.Bool("recursive", false, "generate all Go packages below --dir, in dependency order")
	recover := flags.Bool("recover", false, "recover an interrupted publication in this package")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return cli.Usage("generate accepts flags only; use --dir to select a package")
	}
	if *recover {
		if *check {
			return cli.Usage("--recover and --check cannot be combined")
		}
		if *recursive {
			return cli.Usage("--recover uses the journal's recorded package scope; omit --recursive")
		}
		report, err := generate.Recover(ctx, *dir)
		if err != nil {
			return err
		}
		if !report.Found {
			_, err = fmt.Fprintln(stdout, "No interrupted generation found.")
		} else if report.Kept {
			_, err = fmt.Fprintln(stdout, "Complete generated output retained; staging cleaned.")
		} else if report.RolledBack {
			_, err = fmt.Fprintln(stdout, "Interrupted generated output restored to its previous state.")
		} else {
			_, err = fmt.Fprintln(stdout, "Unpublished generation staging cleaned.")
		}
		return err
	}
	report, err := generate.Generate(ctx, generate.Options{Dir: *dir, Check: *check, Recursive: *recursive})
	if err != nil {
		return err
	}
	if *check {
		_, err := fmt.Fprintln(stdout, "Generated output is current.")
		return err
	}
	_, err = fmt.Fprintf(stdout, "Generated %d file(s); removed %d obsolete owned file(s).\n", len(report.Written), len(report.Removed))
	return err
}

func runAgent(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	const usage = "usage: foundry agent complete|hover|definition --file source.go --line N --column N [--workspace directory] [--insert text] [--format json|text]"
	if handled, err := subcommandHelp(args, stdout, usage); handled {
		return err
	}
	if len(args) == 0 || (args[0] != "complete" && args[0] != "hover" && args[0] != "definition") {
		return cli.Usage(usage)
	}
	flags := flag.NewFlagSet("agent "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := agent.Options{Operation: args[0]}
	flags.StringVar(&options.Workspace, "workspace", ".", "consumer module or workspace directory")
	flags.StringVar(&options.File, "file", "", "existing Go source file, relative to workspace or absolute")
	flags.IntVar(&options.Line, "line", 0, "one-based source line")
	flags.IntVar(&options.Column, "column", 0, "one-based UTF-8 byte column")
	flags.StringVar(&options.Insert, "insert", "", "insert text only in the unsaved buffer and inspect immediately after it")
	flags.StringVar(&options.Gopls, "gopls", "gopls", "existing gopls executable or absolute path")
	format := flags.String("format", "json", "json or text")
	timeout := flags.Duration("timeout", 30*time.Second, "maximum language-server request duration")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return cli.Usage("agent inspection accepts flags only")
	}
	if options.File == "" || options.Line < 1 || options.Column < 1 {
		return cli.Usage("agent requires --file and positive --line and --column")
	}
	if *format != "json" && *format != "text" {
		return cli.Usage("agent format must be json or text")
	}
	if *timeout <= 0 {
		return cli.Usage("agent timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result, err := agent.Inspect(ctx, options)
	if err != nil {
		return err
	}
	if *format == "text" {
		return agent.WriteText(stdout, result)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
