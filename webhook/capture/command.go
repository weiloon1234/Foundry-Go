package capture

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const commandUsage = "webhook-captures prune [--before RFC3339] [--batch 1000] [--format text|json] [--apply]"

// PruneCommand is a validated prune operation. Without --before it applies
// the configured retention and row cap; with it, only the explicit cutoff,
// which must not be later than now. Without --apply it only counts.
type PruneCommand struct {
	before value.Optional[temporal.DateTime]
	batch  int
	json   bool
	apply  bool
	parsed bool
}

// ParsePrune parses prune arguments without database I/O. It writes help on
// request and then returns flag.ErrHelp.
func ParsePrune(args []string, help io.Writer) (PruneCommand, error) {
	if help == nil {
		return PruneCommand{}, invalid("webhook capture commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, commandUsage+"\n"); err != nil {
			return PruneCommand{}, err
		}
		return PruneCommand{}, flag.ErrHelp
	}
	if len(args) < 1 || args[0] != "prune" {
		return PruneCommand{}, clidiag.Usage(commandUsage)
	}
	result := PruneCommand{batch: pruneBatch, parsed: true}
	flags := flag.NewFlagSet("webhook-captures prune", flag.ContinueOnError)
	flags.SetOutput(help)
	flags.Func("before", "remove captures received before this RFC 3339 instant (default: configured retention and row cap)", func(text string) error {
		instant, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return invalid("--before requires an RFC 3339 instant")
		}
		cutoff, err := temporal.NewDateTime(instant)
		result.before = value.Set(cutoff)
		return err
	})
	flags.IntVar(&result.batch, "batch", pruneBatch, fmt.Sprintf("rows removed per transaction (1-%d)", pruneBatch))
	format := flags.String("format", "text", "text or json")
	flags.BoolVar(&result.apply, "apply", false, "remove the matching captures (default: count only)")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return PruneCommand{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return PruneCommand{}, clidiag.Usage(commandUsage)
	}
	if result.batch < 1 || result.batch > pruneBatch {
		return PruneCommand{}, clidiag.Usage(fmt.Sprintf("--batch must be between 1 and %d", pruneBatch))
	}
	result.json = *format == "json"
	return result, nil
}

// Command declares `webhook-captures prune` for the ordinary CLI registry.
// construct resolves a store after boot; it works after capture is disabled
// or removed, as long as the table's database and schema are configured.
//
// Example: capture.Command(func(r foundation.Resolver) (*capture.Store, error) { return foundation.Resolve(r, key) })
func Command(construct func(foundation.Resolver) (*Store, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, invalid("webhook capture command requires a store constructor")
	}
	definition := cli.Define("webhook-captures", "Count or prune stored webhook captures in bounded batches", ParsePrune)
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[PruneCommand], error) {
		captures, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command PruneCommand, streams cli.Streams) error {
			return command.Run(ctx, captures, streams.Out)
		}, nil
	})
}

type pruneReport struct {
	Applied  bool  `json:"applied"`
	Matching int64 `json:"matching,omitzero"`
	Removed  int64 `json:"removed"`
}

// Run counts the matching captures or, with --apply, removes them in bounded
// transactions and reports the committed count, also when a later batch
// fails. It never prints captured data.
func (c PruneCommand) Run(ctx context.Context, captures *Store, output io.Writer) error {
	if ctx == nil || captures == nil || output == nil || !c.parsed {
		return invalid("uninitialized webhook capture command; use ParsePrune")
	}
	now, err := temporal.NewDateTime(captures.clock.Now())
	if err != nil {
		return err
	}
	cutoff, explicit := c.before.Get()
	if !explicit {
		if cutoff, err = captures.retentionCutoff(); err != nil {
			return err
		}
	}
	if cutoff.UTC().After(now.UTC()) {
		return clidiag.Usage("--before must not be later than the current time")
	}
	report := pruneReport{Applied: c.apply}
	if c.apply {
		var done bool
		for !done && err == nil {
			var removed int64
			var behind bool
			removed, behind, err = captures.prune(ctx, cutoff, !explicit, c.batch, pruneBatches)
			report.Removed += removed
			done = !behind
		}
	} else {
		report.Matching, err = captures.pruneEstimate(ctx, cutoff, !explicit)
	}
	if err != nil && !c.apply {
		return err
	}
	if c.json {
		if writeErr := json.NewEncoder(output).Encode(report); writeErr != nil {
			return writeErr
		}
	} else if c.apply {
		if _, writeErr := fmt.Fprintf(output, "removed %d webhook captures\n", report.Removed); writeErr != nil {
			return writeErr
		}
	} else if _, writeErr := fmt.Fprintf(output, "%d webhook captures match; rerun with --apply to remove them\n", report.Matching); writeErr != nil {
		return writeErr
	}
	return err
}
