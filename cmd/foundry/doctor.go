package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/doctor"
)

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options doctor.Options
	flags.StringVar(&options.Dir, "dir", ".", "existing consumer module directory")
	flags.StringVar(&options.Go, "go", "go", "installed Go executable")
	flags.StringVar(&options.Gopls, "gopls", "gopls", "installed gopls executable")
	flags.BoolVar(&options.RequireGopls, "require-gopls", false, "fail when the language server is unavailable")
	flags.DurationVar(&options.Timeout, "timeout", 10*time.Second, "maximum duration of each offline tool inspection")
	format := flags.String("format", "text", "text or json")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") || options.Timeout <= 0 || options.Timeout > time.Minute {
		return cli.Usage("doctor accepts flags only, text/json format, and a positive timeout of at most one minute")
	}
	report, inspectErr := doctor.Inspect(ctx, options)
	if *format == "json" {
		return errors.Join(inspectErr, json.NewEncoder(stdout).Encode(report))
	}
	if _, err := fmt.Fprintf(stdout, "Foundry plugin API %s\n", report.FrameworkAPI); err != nil {
		return errors.Join(inspectErr, err)
	}
	for _, check := range report.Checks {
		status := "ok"
		if !check.Passed {
			status = "optional"
			if check.Required {
				status = "failed"
			}
		}
		if _, err := fmt.Fprintf(stdout, "%s: %s — %s\n", check.Name, status, check.Detail); err != nil {
			return errors.Join(inspectErr, err)
		}
	}
	return inspectErr
}
