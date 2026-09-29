// Package command supplies application-owned job operations. Parse before boot;
// Run borrows the application's configured connections and never owns a worker.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Command is a validated immutable operation. Retry is guarded by the token
// supplied on the command line, including when copied or invoked again.
// Bulk operations (retry-failed, flush-failed, clear) walk the queue in bounded
// pages and require --confirm; forget removes one retained terminal job.
type Command struct {
	operation  string
	connection jobs.ConnectionName
	queue      jobs.Queue
	id         jobs.ExecutionID
	token      jobs.RetryToken
	options    jobs.ListOptions
	confirm    bool
	json       bool
}

const usage = "jobs failed|inspect|retry|stats|retry-failed|flush-failed|forget|clear|migrate-layout [--connection name] [--queue name] [--format text|json]; failed: [--limit 20] [--after id]; inspect|forget: --id id; retry: --id id --token failed-state-token; retry-failed|flush-failed|clear: --confirm [--name job --version n]; migrate-layout: --confirm (after the previous release stopped or drained)"

func bulk(operation string) bool {
	return operation == "retry-failed" || operation == "flush-failed" || operation == "clear"
}

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "job commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(help, usage+"\n")
		if err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "jobs" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{operation: args[1], options: jobs.ListOptions{State: jobs.Failed, Limit: 20}}
	switch result.operation {
	case "failed", "inspect", "retry", "stats", "retry-failed", "flush-failed", "forget", "clear", "migrate-layout":
	default:
		return Command{}, cli.Usage(usage)
	}
	flags := flag.NewFlagSet("jobs "+result.operation, flag.ContinueOnError)
	flags.SetOutput(help)
	flags.Func("connection", "configured job connection (default when omitted)", func(text string) error {
		result.connection = jobs.ConnectionName(text)
		return result.connection.Validate()
	})
	flags.Func("queue", "queue (connection default when omitted)", func(text string) error {
		result.queue = jobs.Queue(text)
		return result.queue.Validate()
	})
	format := flags.String("format", "text", "text or json")
	switch {
	case result.operation == "failed":
		flags.IntVar(&result.options.Limit, "limit", 20, "maximum records in this page (1-100)")
		flags.Func("after", "cursor from the previous page", func(text string) error {
			id, err := model.ParseID[jobs.Execution](text)
			result.options.After = id
			return err
		})
	case bulk(result.operation):
		result.options.Limit = jobs.MaxListLimit
		flags.BoolVar(&result.confirm, "confirm", false, "confirm the bulk operation")
		flags.Func("name", "limit to one job name (with --version)", func(text string) error {
			result.options.Name = jobs.Name(text)
			return nil
		})
		flags.Func("version", "job payload version (with --name)", func(text string) error {
			version, err := strconv.ParseUint(text, 10, 32)
			result.options.Version = jobs.Version(version)
			return err
		})
	case result.operation == "migrate-layout":
		flags.BoolVar(&result.confirm, "confirm", false, "confirm that every process of the previous release stopped or drained")
	case result.operation != "stats":
		flags.Func("id", "retained job execution ID", func(text string) error {
			id, err := model.ParseID[jobs.Execution](text)
			result.id = id
			return err
		})
	}
	if result.operation == "retry" {
		flags.Func("token", "retry_token from a prior failed inspection", func(text string) error {
			result.token = jobs.RetryToken(text)
			return result.token.Validate()
		})
	}
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	if err := result.options.Validate(); err != nil {
		return Command{}, cli.InvalidArguments(err)
	}
	if (result.operation == "inspect" || result.operation == "retry" || result.operation == "forget") && result.id.IsZero() {
		return Command{}, cli.Usage("--id is required")
	}
	if result.operation == "migrate-layout" && !result.confirm {
		return Command{}, cli.Usage("--confirm is required: stop or drain every process of the previous release first")
	}
	if bulk(result.operation) && !result.confirm {
		return Command{}, cli.Usage("--confirm is required for bulk queue operations")
	}
	if result.operation == "retry" && result.token == "" {
		return Command{}, cli.Usage("--token from a failed inspection is required")
	}
	result.json = *format == "json"
	return result, nil
}

// Declaration registers one jobs command in the ordinary CLI registry. The
// constructor resolves configured services after boot; parsing/help does no I/O.
func Declaration(construct func(foundation.Resolver) (*jobs.Connections, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "job commands require a connection constructor")
	}
	definition := cli.Define("jobs", "Inspect, retry and manage retained jobs", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"jobs"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		connections, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, connections, streams.Out)
		}, nil
	})
}

type page struct {
	Jobs []jobs.Summary   `json:"jobs"`
	Next jobs.ExecutionID `json:"next,omitzero"`
}
type inspection struct {
	Job     jobs.Summary      `json:"job"`
	History []jobs.Transition `json:"history"`
}
type retryResult struct {
	ID         jobs.ExecutionID `json:"id"`
	Token      jobs.RetryToken  `json:"token"`
	Acceptance string           `json:"acceptance"`
	Changed    bool             `json:"changed"`
}

// Run emits only operational metadata. It never exposes payloads or error text.
// Retry output always includes the supplied token. Any mutation/output failure
// requires reconciliation with that same token, never a newly fetched one.
func (c Command) Run(ctx context.Context, connections *jobs.Connections, output io.Writer) error {
	if ctx == nil || output == nil {
		return fault.New(fault.Invalid, "job command requires context and output")
	}
	switch c.operation {
	case "failed", "inspect", "retry", "stats", "retry-failed", "flush-failed", "forget", "clear", "migrate-layout":
	default:
		return fault.New(fault.Invalid, "uninitialized job command; use Parse")
	}
	if err := ctx.Err(); err != nil {
		return err
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
	queue := c.queue
	if queue == "" {
		queue = connection.DefaultQueue()
	}
	dispatcher := connection.Dispatcher()
	switch {
	case c.operation == "migrate-layout":
		migrated, err := dispatcher.MigrateLayout(ctx, queue)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(struct {
				Queue    jobs.Queue `json:"queue"`
				Migrated bool       `json:"migrated"`
			}{queue, migrated})
		}
		_, err = fmt.Fprintf(output, "%s\tmigrated=%t\n", queue, migrated)
		return err
	case c.operation == "stats":
		stats, err := dispatcher.Stats(ctx, queue)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(stats)
		}
		_, err = fmt.Fprintf(output, "waiting=%d delayed=%d blocked=%d leased=%d failed=%d retained=%d\n", stats.Waiting, stats.Delayed, stats.Blocked, stats.Leased, stats.Failed, stats.Retained)
		return err
	case bulk(c.operation):
		return c.runBulk(ctx, dispatcher, queue, output)
	}
	if c.operation == "failed" {
		found, err := dispatcher.List(ctx, queue, c.options)
		if err != nil {
			return err
		}
		result := page{Jobs: make([]jobs.Summary, 0, len(found.Records)), Next: found.Next}
		for _, record := range found.Records {
			result.Jobs = append(result.Jobs, record.Summary())
		}
		if c.json {
			return json.NewEncoder(output).Encode(result)
		}
		for _, job := range result.Jobs {
			if err := writeSummary(output, job); err != nil {
				return err
			}
		}
		if !result.Next.IsZero() {
			_, err = fmt.Fprintln(output, "next:", result.Next.String())
		}
		return err
	}
	found, err := dispatcher.Inspect(ctx, queue, c.id)
	if err != nil {
		return err
	}
	record, ok := found.Get()
	if !ok {
		return fault.New(fault.Missing, "job is not retained in the selected queue")
	}
	if c.operation == "forget" {
		changed, err := dispatcher.Forget(ctx, queue, record.Envelope.Target())
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(struct {
				ID      jobs.ExecutionID `json:"id"`
				Changed bool             `json:"changed"`
			}{c.id, changed})
		}
		_, err = fmt.Fprintf(output, "%s\tforgotten=%t\n", c.id.String(), changed)
		return err
	}
	if c.operation == "inspect" {
		if c.json {
			return json.NewEncoder(output).Encode(inspection{record.Summary(), record.History})
		}
		if err := writeSummary(output, record.Summary()); err != nil {
			return err
		}
		for _, transition := range record.History {
			if _, err := fmt.Fprintf(output, "%s\t%s\tretry=%d attempt=%d\t%s\n", transition.At.UTC().Format(time.RFC3339Nano), transition.State, transition.Retry, transition.Attempt, transition.Reason); err != nil {
				return err
			}
		}
		return nil
	}
	changed, retryErr := dispatcher.Retry(ctx, queue, jobs.RetryRequest{Target: record.Envelope.Target(), Token: c.token})
	result := retryResult{ID: c.id, Token: c.token, Acceptance: "unknown", Changed: changed}
	if retryErr == nil {
		result.Acceptance = "confirmed"
	}
	if c.json {
		err = json.NewEncoder(output).Encode(result)
	} else {
		_, err = fmt.Fprintf(output, "%s\tacceptance=%s changed=%t token=%s\n", result.ID.String(), result.Acceptance, result.Changed, result.Token)
	}
	return errors.Join(retryErr, err)
}

// runBulk walks the whole queue in bounded pages. Each page's changes are
// confirmed independently; an error leaves earlier pages applied.
func (c Command) runBulk(ctx context.Context, dispatcher *jobs.Dispatcher, queue jobs.Queue, output io.Writer) error {
	total := jobs.BulkResult{}
	options := c.options
	for {
		var page jobs.BulkResult
		var err error
		switch c.operation {
		case "retry-failed":
			page, err = dispatcher.RetryFailed(ctx, queue, options)
		case "flush-failed":
			page, err = dispatcher.FlushFailed(ctx, queue, options)
		default:
			page, err = dispatcher.Clear(ctx, queue, options)
		}
		total.Matched += page.Matched
		total.Changed += page.Changed
		if err != nil || page.Next.IsZero() {
			var writeErr error
			if c.json {
				writeErr = json.NewEncoder(output).Encode(struct {
					Operation string `json:"operation"`
					Matched   int    `json:"matched"`
					Changed   int    `json:"changed"`
				}{c.operation, total.Matched, total.Changed})
			} else {
				_, writeErr = fmt.Fprintf(output, "%s matched=%d changed=%d\n", c.operation, total.Matched, total.Changed)
			}
			return errors.Join(err, writeErr)
		}
		options.After = page.Next
	}
}

func writeSummary(out io.Writer, job jobs.Summary) error {
	_, err := fmt.Fprintf(out, "%s\t%s v%d\t%s\tattempts=%d/%d retries=%d\t%s\ttoken=%s\n", job.ID.String(), job.Name, job.Version, job.State, job.Attempts, job.MaxAttempts, job.Retries, job.Reason, job.RetryToken)
	return err
}
