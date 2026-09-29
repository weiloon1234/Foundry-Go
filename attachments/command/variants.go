package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

const variantUsage = "attachments variants --owner name --collection name [--missing] [--batch 20] [--format text|json]"

// VariantCommand regenerates a collection's declared image variants for every
// ready attachment, in bounded batches until the collection is exhausted.
type VariantCommand struct {
	owner      extensions.OwnerName
	collection attachments.Name
	missing    bool
	batch      int
	json       bool
}

// ParseVariants validates the command before boot and performs no I/O.
func ParseVariants(args []string, help io.Writer) (VariantCommand, error) {
	if help == nil {
		return VariantCommand{}, fault.New(fault.Invalid, "attachment variant commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, "usage: "+variantUsage+"\n"); err != nil {
			return VariantCommand{}, err
		}
		return VariantCommand{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "attachments" || args[1] != "variants" {
		return VariantCommand{}, cli.Usage(variantUsage)
	}
	flags := flag.NewFlagSet("attachments variants", flag.ContinueOnError)
	flags.SetOutput(help)
	owner := flags.String("owner", "", "registered model owner")
	collection := flags.String("collection", "", "registered collection with variants")
	missing := flags.Bool("missing", false, "generate only variants that do not exist yet")
	batch := flags.Int("batch", 20, "attachments per batch (1-100)")
	format := flags.String("format", "text", "text or json")
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return VariantCommand{}, err
	}
	if flags.NArg() != 0 || !identifier.Semantic(*owner) || !identifier.Semantic(*collection) || *format != "text" && *format != "json" {
		return VariantCommand{}, cli.Usage(variantUsage)
	}
	if *batch < 1 || *batch > attachments.MaxRegenerationBatch {
		return VariantCommand{}, cli.Usage("--batch must be between 1 and 100")
	}
	return VariantCommand{owner: extensions.OwnerName(*owner), collection: attachments.Name(*collection), missing: *missing, batch: *batch, json: *format == "json"}, nil
}

// VariantDeclaration registers the command in the ordinary CLI registry as
// "attachment-variants". The manager is resolved after boot.
func VariantDeclaration(construct func(foundation.Resolver) (*attachments.Manager, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "attachment variant commands require a manager constructor")
	}
	definition := cli.Define("attachment-variants", "Regenerate declared attachment image variants in bounded batches", func(args []string, help io.Writer) (VariantCommand, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return ParseVariants(args, help)
		}
		return ParseVariants(append([]string{"attachments", "variants"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[VariantCommand], error) {
		manager, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command VariantCommand, streams cli.Streams) error {
			return command.Run(ctx, manager, streams.Out)
		}, nil
	})
}

type variantReport struct {
	Regenerated int      `json:"regenerated"`
	Failed      []string `json:"failed"`
}

// Run reports counts and failed operation IDs only, never file contents,
// storage keys or error text. Any failed attachment makes the command fail
// after the whole collection was processed.
func (c VariantCommand) Run(ctx context.Context, manager *attachments.Manager, out io.Writer) error {
	if ctx == nil || out == nil || manager == nil || c.batch < 1 {
		return fault.New(fault.Invalid, "attachment variant command requires context, manager and output")
	}
	report := variantReport{Failed: []string{}}
	options := attachments.RegenerationOptions{Missing: c.missing, Limit: c.batch}
	for {
		page, err := manager.RegenerateVariants(ctx, c.owner, c.collection, options)
		report.Regenerated += len(page.Regenerated)
		for _, id := range page.Failed {
			report.Failed = append(report.Failed, id.String())
		}
		if err != nil {
			_ = c.report(out, report)
			return err
		}
		if page.Next.IsZero() {
			break
		}
		options.Cursor = page.Next
	}
	if err := c.report(out, report); err != nil {
		return err
	}
	if len(report.Failed) > 0 {
		return fault.New(fault.Conflict, "some attachments failed variant generation")
	}
	return nil
}
func (c VariantCommand) report(out io.Writer, report variantReport) error {
	if c.json {
		return json.NewEncoder(out).Encode(report)
	}
	if _, err := fmt.Fprintf(out, "regenerated=%d failed=%d\n", report.Regenerated, len(report.Failed)); err != nil {
		return err
	}
	for _, id := range report.Failed {
		if _, err := fmt.Fprintf(out, "failed\t%s\n", id); err != nil {
			return err
		}
	}
	return nil
}
