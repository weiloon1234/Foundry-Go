// Package command supplies outbound webhook delivery inspection and replay.
// Parse before boot; Run borrows the application's configured delivery queue.
package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
)

const usage = "webhooks deliveries|replay|prune [--format text|json]; deliveries: [--endpoint id] [--state pending|succeeded|failed] [--limit 50]; replay: --delivery id | --failed [--endpoint id] [--limit 100]; prune: --older-than duration [--limit 1000]"

// Command is a validated immutable operation.
type Command struct {
	operation string
	endpoint  value.Optional[outbound.EndpointID]
	delivery  outbound.DeliveryID
	state     value.Optional[outbound.DeliveryState]
	failed    bool
	olderThan time.Duration
	limit     int
	json      bool
}

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "webhook commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "webhooks" || args[1] != "deliveries" && args[1] != "replay" && args[1] != "prune" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{operation: args[1]}
	flags := flag.NewFlagSet("webhooks "+result.operation, flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	if result.operation != "prune" {
		flags.Func("endpoint", "endpoint ID", func(text string) error {
			id, err := model.ParseID[outbound.Endpoint](text)
			result.endpoint = value.Set(id)
			return err
		})
	}
	switch result.operation {
	case "prune":
		flags.DurationVar(&result.olderThan, "older-than", 0, "prune finished deliveries last updated before now minus this duration (at least 1h)")
		flags.IntVar(&result.limit, "limit", 1000, "deliveries per transaction (1-10000)")
	case "deliveries":
		flags.Func("state", "pending, succeeded or failed", func(text string) error {
			state := outbound.DeliveryState(text)
			if state != outbound.Pending && state != outbound.Succeeded && state != outbound.Failed {
				return fault.New(fault.Invalid, "invalid delivery state")
			}
			result.state = value.Set(state)
			return nil
		})
		flags.IntVar(&result.limit, "limit", 50, "maximum rows (1-1000)")
	default:
		flags.Func("delivery", "delivery ID to replay", func(text string) error {
			id, err := model.ParseID[outbound.Delivery](text)
			result.delivery = id
			return err
		})
		flags.BoolVar(&result.failed, "failed", false, "replay failed deliveries")
		flags.IntVar(&result.limit, "limit", 100, "failed deliveries per run (1-1000)")
	}
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	maximum := 1000
	if result.operation == "prune" {
		maximum = outbound.MaxPruneBatch
		if result.olderThan < time.Hour {
			return Command{}, cli.Usage("prune requires --older-than of at least 1h")
		}
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") || result.limit < 1 || result.limit > maximum {
		return Command{}, cli.Usage(usage)
	}
	if result.operation == "replay" && (result.failed == !result.delivery.IsZero() || !result.delivery.IsZero() && result.endpoint.IsSet()) {
		return Command{}, cli.Usage("replay requires exactly one of --delivery or --failed")
	}
	result.json = *format == "json"
	return result, nil
}

// Declaration registers the command as "webhooks". The queue is resolved
// after boot; parsing performs no I/O.
func Declaration(construct func(foundation.Resolver) (outbound.Queue, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "webhook commands require a queue constructor")
	}
	definition := cli.Define("webhooks", "Inspect, replay and prune outbound webhook deliveries", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"webhooks"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		queue, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, queue, streams.Out)
		}, nil
	})
}

type deliveryRow struct {
	ID          string `json:"id"`
	Endpoint    string `json:"endpoint"`
	Event       string `json:"event"`
	State       string `json:"state"`
	Attempts    uint32 `json:"attempts"`
	LastStatus  int    `json:"last_status"`
	LastFailure string `json:"last_failure"`
	CreatedAt   string `json:"created_at"`
}

// Run emits delivery metadata only: never payloads, URLs, secrets or errors.
func (c Command) Run(ctx context.Context, queue outbound.Queue, out io.Writer) error {
	if ctx == nil || out == nil || c.operation == "" {
		return fault.New(fault.Invalid, "webhook command requires context, queue and output; use Parse")
	}
	if err := queue.Validate(); err != nil {
		return err
	}
	switch c.operation {
	case "deliveries":
		rows, err := queue.Service().Deliveries(ctx, outbound.DeliveryQuery{Endpoint: c.endpoint, State: c.state, Limit: c.limit})
		if err != nil {
			return err
		}
		listed := make([]deliveryRow, 0, len(rows))
		for _, row := range rows {
			listed = append(listed, deliveryRow{ID: row.ID.String(), Endpoint: row.Endpoint.String(), Event: string(row.Event), State: string(row.State), Attempts: row.Attempts, LastStatus: row.LastStatus, LastFailure: row.LastFailure, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)})
		}
		if c.json {
			return json.NewEncoder(out).Encode(struct {
				Deliveries []deliveryRow `json:"deliveries"`
			}{listed})
		}
		for _, row := range listed {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\tattempts=%d\tstatus=%d\t%s\t%s\n", row.ID, row.Endpoint, row.Event, row.State, row.Attempts, row.LastStatus, row.LastFailure, row.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	case "replay":
		count := 1
		var err error
		if c.failed {
			count, err = queue.ReplayFailed(ctx, c.endpoint, c.limit)
		} else {
			err = queue.Replay(ctx, c.delivery)
		}
		if err != nil {
			return err
		}
		return c.report(out, "replayed", count)
	case "prune":
		// Repeat bounded batches so each transaction holds its locks briefly.
		total := 0
		for {
			count, err := queue.Service().PruneDeliveries(ctx, c.olderThan, c.limit)
			total += count
			if err != nil {
				_ = c.report(out, "pruned", total)
				return err
			}
			if count < c.limit {
				return c.report(out, "pruned", total)
			}
		}
	}
	return fault.New(fault.Invalid, "uninitialized webhook command; use Parse")
}
func (c Command) report(out io.Writer, name string, count int) error {
	if c.json {
		return json.NewEncoder(out).Encode(map[string]int{name: count})
	}
	_, err := fmt.Fprintf(out, "%s=%d\n", name, count)
	return err
}
