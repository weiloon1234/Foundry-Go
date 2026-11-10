package bootstrap

import (
	"context"
	"flag"
	"strings"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
)

// RunDeployJobs is the consumer's restricted jobs operations entry point. Global settings
// flags are removed before the framework parses command-local flags.
func RunDeployJobs(ctx context.Context, args []string, streams cli.Streams) error {
	globals := flag.NewFlagSet("artisan", flag.ContinueOnError)
	globals.SetOutput(streams.Err)
	path := globals.String("config", "", "TOML configuration path")
	envFile := globals.String("env-file", "", "deployment environment file")
	var globalArgs, commandArgs []string
	for i := 0; i < len(args); i++ {
		name, _, inline := strings.Cut(args[i], "=")
		if name != "--config" && name != "--env-file" {
			commandArgs = append(commandArgs, args[i])
			continue
		}
		globalArgs = append(globalArgs, args[i])
		if !inline {
			i++
			if i == len(args) {
				return cli.Usage("global settings flag requires a path")
			}
			globalArgs = append(globalArgs, args[i])
		}
	}
	if err := cli.ParseFlags(globals, globalArgs); err != nil {
		return err
	}
	settings, err := LoadWithEnvFile(*path, *envFile)
	if err != nil {
		return err
	}
	declarations, err := application.DeployJobCommands(application.New(settings.App))
	if err != nil {
		return err
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		return err
	}
	invocation, err := registry.Parse(commandArgs, streams.Out)
	if err != nil {
		return err
	}
	return invocation.Run(ctx, nil, streams)
}
