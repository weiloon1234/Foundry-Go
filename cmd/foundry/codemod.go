package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/codemod"
)

func runCodemod(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("codemod", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "consumer directory with the current v0.1 dependency")
	apply := flags.Bool("apply", false, "apply resolved v0.2 migration edits; refuse if unresolved cases remain")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: foundry codemod [--dir directory] [--apply] [./...]")
		flags.PrintDefaults()
	}
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	plan, err := codemod.Inventory(ctx, codemod.Options{Dir: *dir, Patterns: flags.Args()}, codemod.Validation{})
	if err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if _, err := fmt.Fprintf(stdout, "%s:%d: %s: %s\n", entry.File, entry.Line, entry.Status, entry.Message); err != nil {
			return err
		}
	}
	if *apply {
		count, err := plan.Apply()
		if err != nil {
			return fmt.Errorf("codemod: applied %d file(s): %w", count, err)
		}
		_, err = fmt.Fprintf(stdout, "Applied %d file(s). Regenerate listed generated files after updating the dependency.\n", count)
		return err
	}
	_, err = fmt.Fprintf(stdout, "Dry run: %d file(s) would change. Use --apply to write.\n", len(plan.Changes))
	return err
}
