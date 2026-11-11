package capturelog

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
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Pruner is the retention surface the prune command drives. Every Log
// implements it.
type Pruner interface {
	Label() string
	Now() (temporal.DateTime, error)
	Cutoff() (temporal.DateTime, error)
	Estimate(ctx context.Context, cutoff temporal.DateTime, capped bool) (int64, error)
	Prune(ctx context.Context, cutoff temporal.DateTime, capped bool, batch, maxBatches int) (int64, bool, error)
}

func usage(name cli.Name) string {
	return string(name) + " prune [--before RFC3339] [--batch 1000] [--format text|json] [--apply]"
}

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
func ParsePrune(name cli.Name, args []string, help io.Writer) (PruneCommand, error) {
	if help == nil {
		return PruneCommand{}, safeerror.NewFault(fault.Invalid, "capture commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage(name)+"\n"); err != nil {
			return PruneCommand{}, err
		}
		return PruneCommand{}, flag.ErrHelp
	}
	if len(args) < 1 || args[0] != "prune" {
		return PruneCommand{}, clidiag.Usage(usage(name))
	}
	result := PruneCommand{batch: PruneBatch, parsed: true}
	flags := flag.NewFlagSet(string(name)+" prune", flag.ContinueOnError)
	flags.SetOutput(help)
	flags.Func("before", "remove captures made before this RFC 3339 instant (default: configured retention and row cap)", func(text string) error {
		instant, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return safeerror.NewFault(fault.Invalid, "--before requires an RFC 3339 instant")
		}
		cutoff, err := temporal.NewDateTime(instant)
		result.before = value.Set(cutoff)
		return err
	})
	flags.IntVar(&result.batch, "batch", PruneBatch, fmt.Sprintf("rows removed per transaction (1-%d)", PruneBatch))
	format := flags.String("format", "text", "text or json")
	flags.BoolVar(&result.apply, "apply", false, "remove the matching captures (default: count only)")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return PruneCommand{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return PruneCommand{}, clidiag.Usage(usage(name))
	}
	if result.batch < 1 || result.batch > PruneBatch {
		return PruneCommand{}, clidiag.Usage(fmt.Sprintf("--batch must be between 1 and %d", PruneBatch))
	}
	result.json = *format == "json"
	return result, nil
}

// Command declares `<name> prune` for the ordinary CLI registry. construct
// resolves a read-and-prune log after boot; it works while capture is
// disabled, as long as the table's database and schema are configured.
func Command(name cli.Name, summary string, construct func(foundation.Resolver) (Pruner, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, safeerror.NewFault(fault.Invalid, "capture command requires a log constructor")
	}
	definition := cli.Define(name, summary, func(args []string, help io.Writer) (PruneCommand, error) {
		return ParsePrune(name, args, help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[PruneCommand], error) {
		target, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command PruneCommand, streams cli.Streams) error {
			return command.Run(ctx, target, streams.Out)
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
func (c PruneCommand) Run(ctx context.Context, target Pruner, output io.Writer) error {
	if ctx == nil || target == nil || output == nil || !c.parsed {
		return safeerror.NewFault(fault.Invalid, "uninitialized capture command; use ParsePrune")
	}
	now, err := target.Now()
	if err != nil {
		return err
	}
	cutoff, explicit := c.before.Get()
	if !explicit {
		if cutoff, err = target.Cutoff(); err != nil {
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
			removed, behind, err = target.Prune(ctx, cutoff, !explicit, c.batch, PruneBatches)
			report.Removed += removed
			done = !behind
		}
	} else {
		report.Matching, err = target.Estimate(ctx, cutoff, !explicit)
	}
	if err != nil && !c.apply {
		return err
	}
	if c.json {
		if writeErr := json.NewEncoder(output).Encode(report); writeErr != nil {
			return writeErr
		}
	} else if c.apply {
		if _, writeErr := fmt.Fprintf(output, "removed %d %ss\n", report.Removed, target.Label()); writeErr != nil {
			return writeErr
		}
	} else if _, writeErr := fmt.Fprintf(output, "%d %ss match; rerun with --apply to remove them\n", report.Matching, target.Label()); writeErr != nil {
		return writeErr
	}
	return err
}
