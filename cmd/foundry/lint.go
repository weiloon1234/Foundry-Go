package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/noraw"
)

func runLint(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("lint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noRaw := flags.Bool("no-raw", false, "best-effort source-policy check for raw SQL calls (including signature-matched function values); not a runtime proof")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if !*noRaw {
		return cli.Usage("lint requires --no-raw; usage: foundry lint --no-raw ./...")
	}
	diagnostics, err := noraw.Check(ctx, ".", flags.Args())
	if err != nil {
		return err
	}
	for _, d := range diagnostics {
		if _, err := fmt.Fprintln(stdout, d.String()); err != nil {
			return err
		}
	}
	if len(diagnostics) > 0 {
		return fmt.Errorf("no-raw: %d raw SQL call site(s)", len(diagnostics))
	}
	return nil
}
