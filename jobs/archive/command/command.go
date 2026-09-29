// Package command supplies the application-owned failed-job archive commands.
// Parse before boot; Run borrows the configured archive and job connections.
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
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Command is a validated immutable operation.
type Command struct {
	operation  string
	options    archive.ListOptions
	id         archive.ID
	connection jobs.ConnectionName
	olderThan  time.Duration
	json       bool
}

const usage = "failed-jobs list|retry|prune [--format text|json]; list: [--name job] [--limit 20] [--after cursor]; retry: --id id [--connection name]; prune: --older-than duration [--limit 1000]"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "failed-job commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "failed-jobs" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{operation: args[1]}
	switch result.operation {
	case "list", "retry", "prune":
	default:
		return Command{}, cli.Usage(usage)
	}
	flags := flag.NewFlagSet("failed-jobs "+result.operation, flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	switch result.operation {
	case "list":
		flags.IntVar(&result.options.Limit, "limit", 20, "maximum entries (1-100)")
		flags.Func("name", "job name", func(text string) error {
			if !identifier.Semantic(text) {
				return fault.New(fault.Invalid, "invalid job name")
			}
			result.options.Name = jobs.Name(text)
			return nil
		})
		flags.Func("after", "cursor from the previous page", func(text string) error {
			cursor, err := archive.ParseCursor(text)
			result.options.After = cursor
			return err
		})
	case "retry":
		flags.Func("id", "archived failure ID", func(text string) error {
			id, err := model.ParseID[archive.Archived](text)
			result.id = id
			return err
		})
		flags.Func("connection", "job connection (default: the default connection)", func(text string) error {
			result.connection = jobs.ConnectionName(text)
			return result.connection.Validate()
		})
	case "prune":
		flags.DurationVar(&result.olderThan, "older-than", 0, "prune entries archived before now minus this duration")
		flags.IntVar(&result.options.Limit, "limit", 1000, "entries per transaction (1-10000)")
	}
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	result.json = *format == "json"
	switch {
	case result.operation == "list" && (result.options.Limit < 1 || result.options.Limit > archive.MaxPage):
		return Command{}, cli.Usage("--limit must be between 1 and 100")
	case result.operation == "retry" && result.id.IsZero():
		return Command{}, cli.Usage("--id is required")
	case result.operation == "prune" && result.olderThan <= 0:
		return Command{}, cli.Usage("--older-than must be a positive duration")
	case result.operation == "prune" && (result.options.Limit < 1 || result.options.Limit > archive.MaxPruneBatch):
		return Command{}, cli.Usage("--limit must be between 1 and 10000")
	}
	return result, nil
}

// Declaration registers the failed-jobs command in the ordinary CLI registry.
func Declaration(construct func(foundation.Resolver) (*archive.Store, *jobs.Connections, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "failed-job commands require an archive constructor")
	}
	definition := cli.Define("failed-jobs", "List, re-dispatch and prune archived failed jobs", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"failed-jobs"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		store, connections, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, store, connections, streams.Out)
		}, nil
	})
}

// Run emits only operational metadata: never payloads or error text.
func (c Command) Run(ctx context.Context, store *archive.Store, connections *jobs.Connections, output io.Writer) error {
	if ctx == nil || output == nil || store == nil {
		return fault.New(fault.Invalid, "failed-job command requires context, archive and output")
	}
	switch c.operation {
	case "list":
		page, err := store.List(ctx, c.options)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(page)
		}
		for _, entry := range page.Entries {
			if _, err := fmt.Fprintf(output, "%s\t%s v%d\t%s\t%s\tattempts=%d exceptions=%d\t%s\n", entry.ID.String(), entry.Name, entry.Version, entry.Queue, entry.Reason, entry.Attempts, entry.Exceptions, entry.FailedAt.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		if !page.Next.IsZero() {
			_, err = fmt.Fprintf(output, "next=%s\n", page.Next.String())
		}
		return err
	case "retry":
		if connections == nil {
			return fault.New(fault.Invalid, "failed-job retry requires job connections")
		}
		var connection *jobs.Connection
		var err error
		if c.connection == "" {
			connection, err = connections.Default()
		} else {
			connection, err = connections.Connection(c.connection)
		}
		if err != nil {
			return err
		}
		execution, err := store.Retry(ctx, connection.Dispatcher(), c.id)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(struct {
				ID  archive.ID       `json:"id"`
				Job jobs.ExecutionID `json:"job"`
			}{c.id, execution})
		}
		_, err = fmt.Fprintf(output, "%s\tjob=%s\n", c.id.String(), execution.String())
		return err
	case "prune":
		total := 0
		for {
			count, err := store.Prune(ctx, time.Now().Add(-c.olderThan), c.options.Limit)
			total += count
			if err != nil || count < c.options.Limit {
				if c.json {
					return errors.Join(err, json.NewEncoder(output).Encode(struct {
						Deleted int `json:"deleted"`
					}{total}))
				}
				_, writeErr := fmt.Fprintf(output, "deleted=%d\n", total)
				return errors.Join(err, writeErr)
			}
		}
	}
	return fault.New(fault.Invalid, "uninitialized failed-job command; use Parse")
}
