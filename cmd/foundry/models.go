package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/modelcheck"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func runModels(ctx context.Context, args []string, stdout, stderr io.Writer) (resultErr error) {
	const usage = "usage: foundry models check [--dir .] [--timeout 5m] [./...]\nCheck that table-backed models are declared only through the generator, that the CHECK value list of an enum column in the migrations equals its enum, and that every migration function is registered. No files are changed.\n--timeout bounds how long the CLI waits, including parsing and type checking; expiry exits with status 124."
	if handled, err := subcommandHelp(args, stdout, usage); handled {
		return err
	}
	if len(args) == 0 || args[0] != "check" {
		return clidiag.Usage(usage)
	}
	flags := flag.NewFlagSet("models check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "consumer module or package directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum CLI wait for loading and analysis; expiry exits with status 124")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return err
	}
	if *timeout <= 0 {
		return clidiag.Usage("models check timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	defer func() {
		if ctx.Err() != nil {
			resultErr = ctx.Err()
		}
	}()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	diagnostics, err := modelcheck.Check(ctx, *dir, flags.Args())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if err := printFindings(stdout, *dir, diagnostics); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(diagnostics) > 0 {
		return safeerror.New(fmt.Sprintf("models check: %d finding(s); each names the guide that teaches its fix", len(diagnostics)))
	}
	_, err = fmt.Fprintln(stdout, "Model tables are declared through the generator, no CHECK constraint rejects a case of an enum column and every migration is registered.")
	return err
}
