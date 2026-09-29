// Package command supplies the application-owned `schedule test` operation.
// Parse before boot; Run borrows the application's configured scheduler and
// invokes one schedule immediately without starting the Scheduler kernel.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

// Command is a validated immutable operation.
type Command struct {
	id   schedule.ID
	json bool
}

const usage = "schedule test --id schedule-id [--format text|json]"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "schedule commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "schedule" || args[1] != "test" {
		return Command{}, cli.Usage(usage)
	}
	var result Command
	flags := flag.NewFlagSet("schedule test", flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	flags.Func("id", "registered schedule ID", func(text string) error {
		if !identifier.Semantic(text) {
			return fault.New(fault.Invalid, "invalid schedule ID")
		}
		result.id = schedule.ID(text)
		return nil
	})
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	if result.id == "" {
		return Command{}, cli.Usage("--id is required")
	}
	result.json = *format == "json"
	return result, nil
}

// Declaration registers the schedule command in the ordinary CLI registry. The
// constructor resolves the configured scheduler after boot; parsing does no I/O.
func Declaration(construct func(foundation.Resolver) (*schedule.Scheduler, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "schedule commands require a scheduler constructor")
	}
	definition := cli.Define("schedule", "Run one registered schedule immediately", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"schedule"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		scheduler, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, scheduler, streams.Out)
		}, nil
	})
}

type outcome struct {
	Schedule   schedule.ID           `json:"schedule"`
	Occurrence schedule.OccurrenceID `json:"occurrence_id"`
	State      schedule.State        `json:"state"`
	Reason     schedule.Reason       `json:"reason,omitempty"`
	Duration   time.Duration         `json:"duration_ns"`
}

// Run emits only the classification: never handler errors or payloads. A
// failed, cancelled or overlap-skipped invocation fails the command after
// reporting its outcome.
func (c Command) Run(ctx context.Context, scheduler *schedule.Scheduler, output io.Writer) error {
	if ctx == nil || output == nil || scheduler == nil || c.id == "" {
		return fault.New(fault.Invalid, "schedule command requires context, scheduler, output and Parse")
	}
	record, err := scheduler.RunNow(ctx, c.id)
	if err != nil {
		return err
	}
	result := outcome{Schedule: c.id, Occurrence: record.Invocation.Occurrence, State: record.State, Reason: record.Reason, Duration: record.FinishedAt.Sub(record.StartedAt)}
	if c.json {
		err = json.NewEncoder(output).Encode(result)
	} else {
		_, err = fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", result.Schedule, result.State, result.Reason, result.Duration)
	}
	// A declined When predicate is a deliberate decision, not a failure.
	if record.State != schedule.Succeeded && record.Reason != schedule.Filtered {
		return errors.Join(err, fault.New(fault.Internal, "schedule invocation did not succeed"))
	}
	return err
}
