package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

func runMake(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	kinds := generate.ScaffoldKinds()
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	usage := "usage: foundry make " + strings.Join(names, "|") + " <GoName> --dir package [kind-specific flags]"
	if handled, err := subcommandHelp(args, stdout, usage); handled {
		return err
	}
	if len(args) == 0 {
		return cli.Usage(usage)
	}
	if !slices.Contains(kinds, generate.ScaffoldKind(args[0])) {
		return cli.Usage(usage)
	}
	var positional string
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		positional = args[1]
		args = append([]string{args[0]}, args[2:]...)
	}
	options := generate.ScaffoldOptions{Kind: generate.ScaffoldKind(args[0])}
	flags := flag.NewFlagSet("make "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.Dir, "dir", ".", "existing consumer Go package directory")
	flags.StringVar(&options.Name, "name", "", "exported Go declaration name")
	if options.Kind != generate.ModelScaffold && options.Kind != generate.DTOScaffold && options.Kind != generate.EnumScaffold {
		flags.StringVar(&options.ID, "id", "", "stable semantic declaration ID")
	}
	if options.Kind == generate.ModelScaffold {
		flags.StringVar(&options.Table, "table", "", "explicit persisted table name")
	}
	if options.Kind == generate.JobScaffold {
		flags.StringVar(&options.Queue, "queue", "default", "typed job routing queue")
	}
	if options.Kind == generate.MigrationScaffold {
		flags.StringVar(&options.Origin, "origin", "", "migration owner, such as app or a plugin namespace")
		flags.StringVar(&options.Version, "version", "", "release introducing this historical migration")
		flags.StringVar(&options.Create, "create", "", "start from a CREATE TABLE statement for this table")
	}
	var cases string
	switch options.Kind {
	case generate.EndpointScaffold:
		flags.StringVar(&options.Method, "method", "POST", "HTTP method: GET, POST, PUT, PATCH or DELETE")
		flags.StringVar(&options.Path, "path", "", "static route path, such as /notes")
	case generate.EnumScaffold:
		flags.StringVar(&cases, "cases", "", "comma-separated lower_snake_case values")
	case generate.ListenerScaffold:
		flags.StringVar(&options.Event, "event", "", "exported event payload type of this package")
	case generate.PolicyScaffold:
		flags.StringVar(&options.Subject, "subject", "", "exported authenticated model type of this package")
		flags.StringVar(&options.Resource, "resource", "", "exported resource type of this package")
	}
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return err
	}
	if flags.NArg() == 1 && positional == "" {
		positional = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return cli.Usage("scaffolding accepts one declaration name")
	}
	if positional != "" {
		if options.Name != "" {
			return cli.Usage("select the declaration with a positional name or --name, not both")
		}
		options.Name = positional
	}
	if cases != "" {
		options.Cases = strings.Split(cases, ",")
	}
	if err := generate.ValidateScaffold(options); err != nil {
		return cli.InvalidArguments(err)
	}
	path, err := generate.Scaffold(ctx, options)
	if err != nil {
		return err
	}
	message := "Implement its domain work, then register its definition explicitly."
	switch options.Kind {
	case generate.ModelScaffold, generate.DTOScaffold:
		message = "Add domain fields, then run foundry generate for this package."
	case generate.EnumScaffold:
		message = "Run foundry generate for this package to create its codecs."
	case generate.EndpointScaffold, generate.NotificationScaffold:
		message = "Its DTOs and generated codecs were created too; add their fields, run foundry generate, then register it."
	}
	if _, err := fmt.Fprintf(stdout, "Created %s. %s\n", path, message); err != nil {
		return fmt.Errorf("scaffold was created at %s but its output could not be written: %w", path, err)
	}
	return nil
}
