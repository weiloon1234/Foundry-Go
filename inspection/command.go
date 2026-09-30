package inspection

import (
	"context"
	"flag"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Command supplies an explicit metadata-only CLI declaration. Parse and run this
// declaration before application Build/Run when inspecting without service
// startup. The pure collector can capture the final command registry, avoiding
// a second declaration list. It must not construct or start services.
func Command(name cli.Name, collect func(context.Context) (Report, error)) (cli.Declaration, error) {
	if collect == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "inspection command requires a pure report collector")
	}
	command := cli.Define(name, "Inspect declared routes, jobs, schedules, plugins, contracts, model extensions and configuration sources", cli.Flags(func(flags *flag.FlagSet, args *Arguments) {
		args.Section = All
		args.Format = Text
		flags.Func("section", "all, routes, jobs, schedules, plugins, commands, configuration, contracts or extensions", func(value string) error { args.Section = Section(value); return nil })
		flags.Func("format", "text or json", func(value string) error { args.Format = Format(value); return nil })
	}, Arguments.Validate))
	return command.Declare(func(foundation.Resolver) (cli.Handler[Arguments], error) {
		return func(ctx context.Context, args Arguments, streams cli.Streams) error {
			report, err := collect(ctx)
			if err != nil {
				return err
			}
			return Write(ctx, streams.Out, report, args)
		}, nil
	})
}
