package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/routestyle"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func runRoutes(ctx context.Context, args []string, stdout, stderr io.Writer) (resultErr error) {
	const usage = "usage: foundry routes check [--dir .] [--timeout 5m] [./...]\nCheck staged route declarations and generated DTO helper use. No files are changed.\n--timeout bounds how long the CLI waits, including parsing and type checking; expiry exits with status 124."
	if handled, err := subcommandHelp(args, stdout, usage); handled {
		return err
	}
	if len(args) == 0 || args[0] != "check" {
		return clidiag.Usage(usage)
	}
	flags := flag.NewFlagSet("routes check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "consumer module or package directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum CLI wait for loading and analysis; expiry exits with status 124")
	if err := cli.ParseFlags(flags, args[1:]); err != nil {
		return err
	}
	if *timeout <= 0 {
		return clidiag.Usage("routes check timeout must be positive")
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
	diagnostics, err := routestyle.Check(ctx, *dir, flags.Args())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	for _, d := range diagnostics {
		if _, err := fmt.Fprintln(stdout, d.String()); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(diagnostics) > 0 {
		return safeerror.New(fmt.Sprintf("routes check: %d finding(s); stage route calls and use generated DTO helpers", len(diagnostics)))
	}
	_, err = fmt.Fprintln(stdout, "Route declarations use the staged style.")
	return err
}
