// Package command supplies the explicit audit retention operation. Parse before
// boot; Run borrows the application's configured audit scope and database.
package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const DefaultBatch = 500

// Command is a validated immutable prune operation. Without --before it uses
// the recorder's configured retention; with it, the explicit RFC 3339 cutoff,
// which must not be later than the current time. Without --apply it only counts
// the matching entries; --apply removes them.
type Command struct {
	before value.Optional[temporal.DateTime]
	area   value.Optional[audit.Area]
	batch  int
	json   bool
	apply  bool
	parsed bool
}

const usage = "audit prune [--before RFC3339] [--area name] [--batch 500] [--format text|json] [--apply]"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "audit commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "audit" || args[1] != "prune" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{batch: DefaultBatch, parsed: true}
	flags := flag.NewFlagSet("audit prune", flag.ContinueOnError)
	flags.SetOutput(help)
	flags.Func("before", "remove entries created before this RFC 3339 instant (default: configured retention)", func(text string) error {
		instant, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return fault.New(fault.Invalid, "--before requires an RFC 3339 instant")
		}
		cutoff, err := temporal.NewDateTime(instant)
		result.before = value.Set(cutoff)
		return err
	})
	flags.Func("area", "audit area (recorder default when omitted)", func(text string) error {
		area := audit.Area(text)
		result.area = value.Set(area)
		return area.Validate()
	})
	flags.IntVar(&result.batch, "batch", DefaultBatch, fmt.Sprintf("rows removed per transaction (1-%d)", query.MaxPageSize))
	format := flags.String("format", "text", "text or json")
	flags.BoolVar(&result.apply, "apply", false, "remove the matching entries (default: count only)")
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	if result.batch < 1 || result.batch > query.MaxPageSize {
		return Command{}, cli.Usage(fmt.Sprintf("--batch must be between 1 and %d", query.MaxPageSize))
	}
	result.json = *format == "json"
	return result, nil
}

// Declaration registers the audit command in the ordinary CLI registry. The
// constructor resolves the configured scope after boot; parsing does no I/O.
func Declaration(construct func(foundation.Resolver) (*audit.Scope, error), now clock.Clock) (cli.Declaration, error) {
	if construct == nil || now == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "audit commands require a scope constructor and clock")
	}
	definition := cli.Define("audit", "Explicitly prune retained audit history in bounded batches", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"audit"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		scope, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, scope, now, streams.Out)
		}, nil
	})
}

type result struct {
	Area     audit.Area `json:"area,omitzero"`
	Applied  bool       `json:"applied"`
	Matching int64      `json:"matching,omitzero"`
	Removed  int64      `json:"removed"`
}

// Run counts the entries older than the cutoff or, with --apply, removes them
// in bounded transactions and reports the committed count, also when a later
// batch fails. A cutoff later than now is rejected before any database work.
// It never prints audit payloads.
func (c Command) Run(ctx context.Context, scope *audit.Scope, now clock.Clock, output io.Writer) error {
	if ctx == nil || scope == nil || now == nil || output == nil || !c.parsed {
		return fault.New(fault.Invalid, "uninitialized audit command; use Parse")
	}
	if area, present := c.area.Get(); present {
		var err error
		if ctx, err = audit.WithArea(ctx, area); err != nil {
			return err
		}
	}
	current, err := temporal.NewDateTime(now.Now())
	if err != nil {
		return err
	}
	cutoff, present := c.before.Get()
	if !present {
		if cutoff, err = scope.RetentionCutoff(current); err != nil {
			return err
		}
	}
	if cutoff.UTC().After(current.UTC()) {
		return cli.Usage("--before must not be later than the current time")
	}
	var count int64
	if c.apply {
		count, err = scope.PruneBefore(ctx, cutoff, c.batch)
	} else if count, err = scope.CountBefore(ctx, cutoff); err != nil {
		return err
	}
	area, _ := c.area.Get()
	report := result{Area: area, Applied: c.apply}
	if c.apply {
		report.Removed = count
	} else {
		report.Matching = count
	}
	var writeErr error
	switch {
	case c.json:
		writeErr = json.NewEncoder(output).Encode(report)
	case c.apply:
		_, writeErr = fmt.Fprintf(output, "removed=%d\n", count)
	default:
		_, writeErr = fmt.Fprintf(output, "matching=%d (dry run; rerun with --apply to remove)\n", count)
	}
	if err != nil {
		return err
	}
	return writeErr
}
