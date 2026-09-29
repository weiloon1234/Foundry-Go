// Package command supplies application-owned outbox operations. Parse before
// boot; Run borrows the application's configured publisher and never starts it.
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
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

// Command is a validated immutable operation. Requeue and prune repeat bounded
// batches until nothing remains, so each transaction holds its locks briefly.
type Command struct {
	operation string
	selection publisher.Selection
	all       bool
	olderThan time.Duration
	json      bool
}

const usage = "outbox stats|failed|requeue|prune [--format text|json]; failed: [--kind kind] [--id id] [--limit 20]; requeue: --all|--kind kind|--id id [--limit 1000]; prune: --older-than duration [--limit 1000]"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "outbox commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "outbox" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{operation: args[1]}
	switch result.operation {
	case "stats", "failed", "requeue", "prune":
	default:
		return Command{}, cli.Usage(usage)
	}
	flags := flag.NewFlagSet("outbox "+result.operation, flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	if result.operation == "failed" || result.operation == "requeue" {
		flags.Func("kind", "producer family, such as job or event", func(text string) error {
			if !identifier.Semantic(text) {
				return fault.New(fault.Invalid, "invalid outbox kind")
			}
			result.selection.Kind = text
			return nil
		})
		flags.Func("id", "outbox message ID", func(text string) error {
			id, err := model.ParseID[publisher.Publication](text)
			result.selection.ID = id
			return err
		})
	}
	switch result.operation {
	case "failed":
		flags.IntVar(&result.selection.Limit, "limit", 20, "maximum rows (1-100)")
	case "requeue":
		flags.BoolVar(&result.all, "all", false, "requeue every failed row")
		flags.IntVar(&result.selection.Limit, "limit", 1000, "rows per transaction (1-10000)")
	case "prune":
		flags.DurationVar(&result.olderThan, "older-than", 0, "prune rows published before now minus this duration")
		flags.IntVar(&result.selection.Limit, "limit", 1000, "rows per transaction (1-10000)")
	}
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	result.json = *format == "json"
	switch result.operation {
	case "failed":
		if result.selection.Limit < 1 || result.selection.Limit > 100 {
			return Command{}, cli.Usage("--limit must be between 1 and 100")
		}
	case "requeue":
		selected := 0
		for _, set := range []bool{result.all, result.selection.Kind != "", !result.selection.ID.IsZero()} {
			if set {
				selected++
			}
		}
		if selected != 1 {
			return Command{}, cli.Usage("requeue requires exactly one of --all, --kind or --id")
		}
		if result.selection.Limit < 1 || result.selection.Limit > publisher.MaxOperationBatch {
			return Command{}, cli.Usage("--limit must be between 1 and 10000")
		}
	case "prune":
		if result.olderThan <= 0 {
			return Command{}, cli.Usage("--older-than must be a positive duration")
		}
		if result.selection.Limit < 1 || result.selection.Limit > publisher.MaxOperationBatch {
			return Command{}, cli.Usage("--limit must be between 1 and 10000")
		}
	}
	return result, nil
}

// Declaration registers the outbox command in the ordinary CLI registry. The
// constructor resolves the configured publisher after boot; parsing does no I/O.
func Declaration(construct func(foundation.Resolver) (*publisher.Publisher, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "outbox commands require a publisher constructor")
	}
	definition := cli.Define("outbox", "Inspect, requeue and prune transactional outbox publication", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"outbox"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		p, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, p, streams.Out)
		}, nil
	})
}

type changed struct {
	Operation string `json:"operation"`
	Rows      int    `json:"rows"`
}

// Run emits only operational metadata: never payloads, origins or error text.
// A failed batch leaves earlier committed batches in place; the reported count
// covers only confirmed batches.
func (c Command) Run(ctx context.Context, p *publisher.Publisher, output io.Writer) error {
	if ctx == nil || output == nil || p == nil {
		return fault.New(fault.Invalid, "outbox command requires context, publisher and output")
	}
	switch c.operation {
	case "stats":
		stats, err := p.Stats(ctx)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(stats)
		}
		_, err = fmt.Fprintf(output, "pending=%d published=%d failed=%d\n", stats.Pending, stats.Published, stats.Failed)
		return err
	case "failed":
		rows, err := p.Failed(ctx, c.selection)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []publisher.Failure{}
		}
		if c.json {
			return json.NewEncoder(output).Encode(struct {
				Failed []publisher.Failure `json:"failed"`
			}{rows})
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(output, "%s\t%s/%s\t%s v%d\tattempts=%d\t%s\t%s\n", row.ID.String(), row.Kind, row.Destination, row.Name, row.Version, row.Attempts, row.Reason, row.CreatedAt.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	case "requeue", "prune":
		// Bound requeue by the failures present at start, so rows that fail
		// again during this command are not requeued repeatedly.
		budget := -1
		if c.operation == "requeue" {
			stats, err := p.Stats(ctx)
			if err != nil {
				return err
			}
			budget = int(min(stats.Failed, int64(^uint(0)>>1)))
		}
		total := 0
		for {
			if budget >= 0 && total >= budget {
				return c.report(output, total)
			}
			var count int
			var err error
			if c.operation == "requeue" {
				count, err = p.Requeue(ctx, c.selection)
			} else {
				count, err = p.Prune(ctx, time.Now().Add(-c.olderThan), c.selection.Limit)
			}
			total += count
			if err != nil {
				_ = c.report(output, total)
				return err
			}
			if count < c.selection.Limit || !c.selection.ID.IsZero() {
				return c.report(output, total)
			}
		}
	}
	return fault.New(fault.Invalid, "uninitialized outbox command; use Parse")
}

func (c Command) report(output io.Writer, rows int) error {
	if c.json {
		return json.NewEncoder(output).Encode(changed{Operation: c.operation, Rows: rows})
	}
	_, err := fmt.Fprintf(output, "%s rows=%d\n", c.operation, rows)
	return err
}
