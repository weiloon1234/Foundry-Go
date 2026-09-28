package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func runContracts(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("contracts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("manifest", "", "manifest exported from the application's registered descriptors")
	var options typescript.Options
	flags.StringVar(&options.Dir, "dir", "", "existing client output directory")
	flags.StringVar(&options.Prefix, "prefix", "contracts", "generated artifact filename prefix")
	flags.StringVar(&options.OpenAPI.Title, "title", "", "application API title")
	flags.StringVar(&options.OpenAPI.APIVersion, "api-version", "", "application API version")
	flags.BoolVar(&options.Check, "check", false, "fail on stale output without writing files or locks")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *input == "" || options.Dir == "" {
		return cli.Usage("contracts requires --manifest and --dir; positional arguments are unsupported")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(*input)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > manifest.MaxBytes {
		_ = file.Close()
		return fmt.Errorf("contract manifest must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, manifest.MaxBytes+1))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	source, err := manifest.Decode(data)
	if err != nil {
		return err
	}
	report, err := typescript.Generate(ctx, source, options)
	if err != nil {
		return err
	}
	if options.Check {
		_, err = fmt.Fprintln(stdout, "Client contracts are current.")
	} else {
		_, err = fmt.Fprintf(stdout, "Generated %d client artifact(s); removed %d obsolete owned file(s).\n", len(report.Written), len(report.Removed))
	}
	return err
}
