package application

import (
	"context"
	"io"
	"log/slog"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	jobcommand "github.com/weiloon1234/Foundry-Go/jobs/command"
)

func (b *Builder) jobCommandSettings() (Settings, options, error) {
	if b == nil || b.state == nil {
		return Settings{}, options{}, safeerror.NewFault(fault.Invalid, "application builder is required")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.built {
		return Settings{}, options{}, safeerror.NewFault(fault.Closed, "application builder is already built")
	}
	return b.state.settings, b.state.options, b.state.err
}

// RunJobCommand executes one jobs/command operation
// against builder by starting only the configured job connections and their
// Redis pools, without starting HTTP, workers, schedulers, outbox relays,
// databases, or business job handlers.
func RunJobCommand(ctx context.Context, builder *Builder, command jobcommand.Command, output io.Writer) (err error) {
	if ctx == nil || builder == nil || output == nil {
		return safeerror.NewFault(fault.Invalid, "job command requires a context, builder and output writer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s, configured, err := builder.jobCommandSettings()
	if err != nil {
		return err
	}
	if err := validateShutdownBudget(s); err != nil {
		return err
	}
	// Restricted commands never acquire application sinks or invoke borrowed log handlers.
	logger := slog.New(slog.DiscardHandler)
	plan, err := infrastructure.ConfigureJobs(s.Services, append(slices.Clone(configured.infrastructure), infrastructure.WithClock(configured.clock), infrastructure.WithLogger(logger))...)
	if err != nil {
		return err
	}
	fBuilder := foundation.NewBuilder(
		foundation.WithClock(configured.clock),
		foundation.WithLogger(logger),
		foundation.WithShutdownTimeout(s.ShutdownTimeout),
		foundation.WithStopDelay(s.StopDelay),
		foundation.WithStartupTimeout(s.StartupTimeout),
	)
	plan.Register(fBuilder)
	app, err := fBuilder.Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
		defer cancel()
		err = safeerror.Join("operation failed", err, app.Shutdown(shutdownCtx))
	}()
	if err := app.Start(ctx); err != nil {
		return err
	}
	resources, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	if err != nil {
		return err
	}
	return command.Run(ctx, resources.Jobs, output)
}

// JobCommand declares a restricted "jobs" CLI command bound to builder. Unlike
// BuiltInCommands, it starts only configured job connections and their Redis
// pools when invoked, without booting HTTP, workers, schedulers, outbox relays,
// databases, or business job handlers.
func JobCommand(builder *Builder) (cli.Declaration, error) {
	if builder == nil {
		return cli.Declaration{}, safeerror.NewFault(fault.Invalid, "job command requires an application builder")
	}
	return cli.Define("jobs", "Inspect, retry and manage retained jobs", func(args []string, help io.Writer) (jobcommand.Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return jobcommand.Parse(args, help)
		}
		return jobcommand.Parse(append([]string{"jobs"}, args...), help)
	}).AllowDuringMaintenance().Declare(func(foundation.Resolver) (cli.Handler[jobcommand.Command], error) {
		return func(ctx context.Context, command jobcommand.Command, streams cli.Streams) error {
			return RunJobCommand(ctx, builder, command, streams.Out)
		}, nil
	})
}

// DeployJobCommands returns the restricted jobs CLI declaration for deployment
// operations without starting application providers or business job handlers.
func DeployJobCommands(builder *Builder) ([]cli.Declaration, error) {
	command, err := JobCommand(builder)
	if err != nil {
		return nil, err
	}
	return []cli.Declaration{command}, nil
}
