package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func globals(args []string) (rest []string, configPath, envFile string, err error) {
	seen := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		name, value, attached := strings.Cut(args[i], "=")
		if name != "--config" && name != "--env-file" {
			rest = append(rest, args[i])
			continue
		}
		if seen[name] {
			return nil, "", "", fmt.Errorf("duplicate %s", name)
		}
		seen[name] = true
		if !attached {
			i++
			if i == len(args) {
				return nil, "", "", fmt.Errorf("%s requires a path", name)
			}
			value = args[i]
		}
		if value == "" || strings.HasPrefix(value, "--") {
			return nil, "", "", fmt.Errorf("%s requires a path", name)
		}
		if name == "--config" {
			configPath = value
		} else {
			envFile = value
		}
	}
	return
}

func Run(ctx context.Context, arguments []string, streams cli.Streams) error {
	args, path, env, err := globals(arguments)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("fixture command is required")
	}
	if args[0] == "feature:relay" {
		return featureRelay(ctx, args[1:])
	}
	settings, report, err := fixtureconfig.Load(path, env)
	if err != nil {
		return err
	}
	receipts := work.NewReceipts("hostdrill")
	if args[0] == "serve" && settings.App.HTTP.Uploads.Enabled {
		if len(args) != 1 {
			return fmt.Errorf("runtime command takes no extra arguments")
		}
		return serveUploads(ctx, settings, receipts)
	}
	builder := Builder(settings, receipts)
	switch args[0] {
	case "config:inspect":
		if len(args) != 1 {
			return fmt.Errorf("config:inspect takes no command flags")
		}
		return json.NewEncoder(streams.Out).Encode(report.Entries())
	case "config:check":
		if len(args) != 1 {
			return fmt.Errorf("config:check takes no command flags")
		}
		built, err := builder.Build(ctx)
		if err != nil {
			return err
		}
		if err := shutdown(built); err != nil {
			return err
		}
		return json.NewEncoder(streams.Out).Encode(struct {
			Valid bool `json:"valid"`
		}{true})
	case "deploy:storage-probe":
		if len(args) != 1 {
			return fmt.Errorf("storage probe takes no command flags")
		}
		return application.ProbeStorage(ctx, builder, streams.Out)
	case "redis:keys":
		if len(args) != 1 {
			return fmt.Errorf("redis:keys takes no command flags")
		}
		inventory, err := builder.RedisKeys()
		if err != nil {
			return err
		}
		return json.NewEncoder(streams.Out).Encode(inventory)
	case "migrate:status", "migrate", "db:seed":
		prefix := []string{"migrate", "up"}
		if args[0] == "migrate:status" {
			prefix[1] = "status"
		}
		if args[0] == "db:seed" {
			prefix = []string{"seed", "run"}
		}
		invocation, err := dbcommand.Parse(append(append(prefix, "--format", "json"), args[1:]...), streams.Err)
		if err != nil {
			return err
		}
		built, err := builder.Build(ctx)
		if err != nil {
			return err
		}
		seeders, err := Seeders()
		if err != nil {
			return errors.Join(err, shutdown(built))
		}
		return errors.Join(built.RunDatabaseCommand(ctx, invocation, application.DatabaseCommandResources{Seeders: seeders}, streams.Out), shutdown(built))
	case "fixture:enqueue":
		return enqueue(ctx, builder, args[1:], streams)
	case "serve", "queue:work", "schedule:work":
		kind := foundation.HTTP
		if args[0] == "queue:work" {
			flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
			flags.SetOutput(streams.Err)
			connection := flags.String("connection", "", "configured job connection")
			if err := cli.ParseFlags(flags, args[1:]); err != nil {
				return err
			}
			if flags.NArg() != 0 {
				return fmt.Errorf("unexpected worker argument")
			}
			if !settings.App.Worker.Enabled || *connection == "" || jobs.ConnectionName(*connection) != settings.App.Worker.Connection {
				return fmt.Errorf("queue:work requires an enabled worker selecting its configured --connection")
			}
			kind = foundation.Worker
		} else if len(args) != 1 {
			return fmt.Errorf("runtime command takes no extra arguments")
		}
		if args[0] == "schedule:work" {
			if !settings.App.Scheduler.Enabled {
				return fmt.Errorf("schedule:work requires an enabled scheduler")
			}
			kind = foundation.Scheduler
		}
		if args[0] == "serve" && !settings.App.HTTP.Enabled {
			return fmt.Errorf("serve requires enabled HTTP")
		}
		built, err := builder.Build(ctx)
		if err != nil {
			return err
		}
		if args[0] == "serve" && settings.Fixture.Readiness == "hold" {
			if err := receipts.Mark("readiness.started"); err != nil {
				return errors.Join(err, shutdown(built))
			}
		}
		return built.Run(ctx, kind)
	default:
		return fmt.Errorf("unknown fixture command %q", args[0])
	}
}

func shutdown(app *application.App) error {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	return app.Shutdown(ctx)
}

func enqueue(ctx context.Context, builder *application.Builder, args []string, streams cli.Streams) (err error) {
	flags := flag.NewFlagSet("fixture:enqueue", flag.ContinueOnError)
	flags.SetOutput(streams.Err)
	queue := flags.String("queue", "", "first or second")
	id := flags.String("id", "", "canonical UUIDv4")
	barrier := flags.Bool("barrier", false, "wait for a release marker")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected enqueue argument")
	}
	definition := First
	switch *queue {
	case "first":
	case "second":
		definition = Second
	default:
		return fmt.Errorf("queue must be first or second")
	}
	if err := work.ValidateID(*id); err != nil {
		return err
	}
	app, err := builder.Build(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, shutdown(app)) }()
	if err := app.Start(ctx); err != nil {
		return err
	}
	connection, err := app.Resources().JobConnection()
	if err != nil {
		return err
	}
	_, err = definition.Dispatch(ctx, connection.Dispatcher(), work.Payload{ID: *id, Barrier: *barrier}, jobs.Options[work.Payload]{Queue: jobs.Queue(*queue)})
	if err != nil {
		return err
	}
	return json.NewEncoder(streams.Out).Encode(struct {
		ID string `json:"id"`
	}{*id})
}
