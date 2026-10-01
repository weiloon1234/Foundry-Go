package typescript

import (
	"context"
	"flag"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

// Sources resolves the application's registered descriptors, such as its HTTP
// router and realtime description, when the export command runs.
type Sources func(context.Context, foundation.Resolver) (manifest.Sources, error)

type exportArguments struct {
	Dir, Prefix string
	Check       bool
	React, Vue  bool
}

// ExportCommand declares an application command that builds the client
// contract from the running application's own registries and publishes the
// TypeScript SDK, manifest and OpenAPI into an existing directory:
//
//	app contracts:export --dir frontend/src/generated [--prefix contracts] [--check]
//
// It replaces a hand-written manifest.Build/Generate command. The API title,
// version and servers come from api, and surfaces adds one client entry per
// portal. Check is read-only and fails on stale output. Building runs no
// handlers or external transports.
func ExportCommand(name cli.Name, api openapi.Options, sources Sources, surfaces ...Surface) (cli.Declaration, error) {
	if sources == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "contract export requires a sources resolver")
	}
	if err := api.Validate(); err != nil {
		return cli.Declaration{}, err
	}
	api.Servers = slices.Clone(api.Servers)
	surfaces, err := validateSurfaces(surfaces)
	if err != nil {
		return cli.Declaration{}, err
	}
	command := cli.Define(name, "Export the typed client contract, TypeScript SDK and OpenAPI", cli.Flags(func(flags *flag.FlagSet, args *exportArguments) {
		flags.StringVar(&args.Dir, "dir", "", "existing client output directory")
		flags.StringVar(&args.Prefix, "prefix", "contracts", "generated artifact filename prefix")
		flags.BoolVar(&args.Check, "check", false, "fail on stale output without writing files or locks")
		flags.BoolVar(&args.React, "react", false, "emit the optional React form subscription adapter")
		flags.BoolVar(&args.Vue, "vue", false, "emit the optional Vue form subscription adapter")
	}, func(args exportArguments) error {
		if args.Dir == "" {
			return fault.New(fault.Invalid, "contract export requires --dir")
		}
		if !prefixPattern.MatchString(args.Prefix) {
			return fault.New(fault.Invalid, "invalid client artifact prefix")
		}
		return nil
	}))
	return command.Declare(func(resolver foundation.Resolver) (cli.Handler[exportArguments], error) {
		return func(ctx context.Context, args exportArguments, streams cli.Streams) error {
			selected, err := sources(ctx, resolver)
			if err != nil {
				return err
			}
			source, err := manifest.Build(ctx, selected)
			if err != nil {
				return err
			}
			report, err := Generate(ctx, source, Options{Dir: args.Dir, Prefix: args.Prefix, Check: args.Check, OpenAPI: api, React: args.React, Vue: args.Vue, Surfaces: surfaces})
			if err != nil {
				return err
			}
			if args.Check {
				_, err = fmt.Fprintln(streams.Out, "Client contracts are current.")
			} else {
				_, err = fmt.Fprintf(streams.Out, "Generated %d client artifact(s); removed %d obsolete owned file(s).\n", len(report.Written), len(report.Removed))
			}
			return err
		}, nil
	})
}
