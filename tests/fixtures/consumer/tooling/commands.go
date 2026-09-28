// Package tooling demonstrates milestone 23 through the public consumer API.
package tooling

import (
	"context"
	"flag"
	"fmt"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/inspection"
)

const InspectName cli.Name = "inspect"

type Greeting struct {
	Name  string
	Count int
}
type Greeter struct{ Prefix string }

var GreeterKey = foundation.NewKey[*Greeter]("tooling.greeter")
var CommandsKey = foundation.NewKey[*cli.Registry]("tooling.commands")

var Greet = cli.Define("greet", "Write a typed greeting", cli.Flags(func(flags *flag.FlagSet, args *Greeting) {
	flags.StringVar(&args.Name, "name", "", "recipient name")
	flags.IntVar(&args.Count, "count", 1, "number of greetings")
}, func(args Greeting) error {
	if args.Name == "" || args.Count < 1 || args.Count > 10 {
		return cli.Usage("--name and a count from 1 to 10 are required")
	}
	return nil
}))

func GreetingDeclaration() (cli.Declaration, error) {
	return Greet.Declare(func(resolver foundation.Resolver) (cli.Handler[Greeting], error) {
		service, err := foundation.Resolve(resolver, GreeterKey)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, args Greeting, streams cli.Streams) error {
			for range args.Count {
				if err := ctx.Err(); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(streams.Out, "%s %s\n", service.Prefix, args.Name); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
}

// Run parses before bootstrap. Metadata inspection deliberately runs its pure
// collector before Build; domain commands use the normal shared CLI kernel.
func Run(ctx context.Context, args []string, streams cli.Streams) error {
	greeting, err := GreetingDeclaration()
	if err != nil {
		return err
	}
	var registry *cli.Registry
	var builder *foundation.Builder
	inspect, err := inspection.Command(InspectName, func(ctx context.Context) (inspection.Report, error) {
		return inspection.Collect(ctx, inspection.Sources{Builder: builder, Commands: registry})
	})
	if err != nil {
		return err
	}
	registry, err = cli.New(greeting, inspect)
	if err != nil {
		return err
	}
	invocation, err := registry.Parse(args, streams.Out)
	if err != nil {
		return err
	}
	service := foundation.Module{Name: "greeter", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, GreeterKey, &Greeter{Prefix: "Hello"})
	}}
	builder = foundry.New().Register(service, cli.Module("cli", CommandsKey, registry, invocation, streams, service.Name))
	if invocation.Name() == InspectName {
		return invocation.Run(ctx, nil, streams)
	}
	app, err := builder.Build(ctx)
	if err != nil {
		return err
	}
	return app.Run(ctx, foundation.CLI)
}
