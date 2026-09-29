// Package command supplies the explicit MFA key-rotation operation. Parse before
// boot; Run borrows the application's configured MFA store and keyring.
package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

const DefaultBatch = 128
const DefaultMaxBatches = 10000

// Command is a validated immutable re-encryption run.
type Command struct {
	batch, maxBatches int
	json, parsed      bool
}

const usage = "mfa reencrypt [--batch 128] [--max-batches 10000] [--format text|json]"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "MFA commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "mfa" || args[1] != "reencrypt" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{parsed: true}
	flags := flag.NewFlagSet("mfa reencrypt", flag.ContinueOnError)
	flags.SetOutput(help)
	flags.IntVar(&result.batch, "batch", DefaultBatch, fmt.Sprintf("factors examined per batch (1-%d)", mfa.MaxRotationBatch))
	flags.IntVar(&result.maxBatches, "max-batches", DefaultMaxBatches, "stop after this many batches (resume by running again)")
	format := flags.String("format", "text", "text or json")
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	if result.batch < 1 || result.batch > mfa.MaxRotationBatch {
		return Command{}, cli.Usage(fmt.Sprintf("--batch must be between 1 and %d", mfa.MaxRotationBatch))
	}
	if result.maxBatches < 1 {
		return Command{}, cli.Usage("--max-batches must be positive")
	}
	result.json = *format == "json"
	return result, nil
}

// Declaration registers `mfa reencrypt` in the ordinary CLI registry. The
// constructor resolves the configured MFA store after boot; parsing does no I/O.
func Declaration(construct func(foundation.Resolver) (*mfa.Store, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "MFA commands require a store constructor")
	}
	definition := cli.Define("mfa", "Re-encrypt stored MFA factors under the active encryption key", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"mfa"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		store, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, store, streams.Out)
		}, nil
	})
}

type report struct {
	Reencrypted uint64 `json:"reencrypted"`
	Changed     uint64 `json:"changed"`
	Failed      uint64 `json:"failed"`
	Complete    bool   `json:"complete"`
}

// Run re-encrypts every factor that does not use the active key, in bounded
// batches, and reports committed counts, also when a later batch fails. Factors
// whose key is no longer retained are counted as failed and make the command
// fail after reporting; restore the retired key and run again. It never prints
// factor material.
func (c Command) Run(ctx context.Context, store *mfa.Store, output io.Writer) error {
	if ctx == nil || store == nil || output == nil || !c.parsed {
		return fault.New(fault.Invalid, "uninitialized MFA command; use Parse")
	}
	var total report
	cursor := mfa.RotationCursor{}
	var runErr error
	for range c.maxBatches {
		batch, err := store.ReencryptStale(ctx, cursor, c.batch)
		total.Reencrypted += batch.Reencrypted
		total.Changed += batch.Changed
		total.Failed += batch.Failed
		if err != nil {
			runErr = err
			break
		}
		cursor = batch.Next
		if batch.Done {
			total.Complete = true
			break
		}
	}
	var writeErr error
	if c.json {
		writeErr = json.NewEncoder(output).Encode(total)
	} else {
		_, writeErr = fmt.Fprintf(output, "reencrypted=%d changed=%d failed=%d complete=%t\n", total.Reencrypted, total.Changed, total.Failed, total.Complete)
	}
	if runErr != nil {
		return runErr
	}
	if total.Failed > 0 {
		return fault.New(fault.Invalid, "some MFA factors use an encryption key that is no longer retained")
	}
	return writeErr
}
