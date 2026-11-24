package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/doctor"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/modelconvert"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func runModelsConvert(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return convertModels(ctx, args, stdout, stderr, modelconvert.GeneratorFunc(func(ctx context.Context, dir string) error {
		_, err := generate.Generate(ctx, generate.Options{Dir: dir, Framework: frameworkinfo.CurrentBuild()})
		return err
	}))
}

func convertModels(ctx context.Context, args []string, stdout, stderr io.Writer, generator modelconvert.Generator) (resultErr error) {
	const usage = "usage: foundry models convert [--dir .] [--dry-run] [--timeout 5m] [./...]\nProve and convert handwritten model declarations, atomically per package."
	if handled, err := subcommandHelp(args, stdout, usage); handled {
		return err
	}
	flags := flag.NewFlagSet("models convert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "consumer module or package directory")
	dry := flags.Bool("dry-run", false, "prove conversions and report refusals without publishing")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum CLI wait; expiry exits with status 124")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if *timeout <= 0 {
		return clidiag.Usage("models convert timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	defer func() {
		if ctx.Err() != nil {
			resultErr = ctx.Err()
		}
	}()
	selected, err := doctor.SelectedFramework(ctx, doctor.Options{Dir: *dir, Go: "go", Timeout: *timeout})
	if err != nil {
		return err
	}
	if _, err := frameworkinfo.CurrentBuild().Check(selected.Selection); err != nil {
		return err
	}
	plans, err := modelconvert.Analyze(ctx, *dir, flags.Args())
	if err != nil {
		return err
	}
	refused, converted := 0, 0
	for _, plan := range plans {
		for _, refusal := range plan.Refused {
			if _, err := fmt.Fprintf(stdout, "%s: model %s: %s\n", refusal.Position, refusal.Model, refusal.Reason); err != nil {
				return err
			}
			refused++
		}
		if len(plan.Refused) > 0 || len(plan.Models) == 0 {
			continue
		}
		verified, err := modelconvert.Prove(ctx, plan, modelconvert.ProofOptions{Generator: generator, FrameworkDirectory: selected.Dir, PublicationValidator: conversionPublisher{}})
		if err != nil {
			message, ok := safeerror.OwnedMessage(err)
			if !ok {
				message = fmt.Sprintf("conversion proof returned an unowned %T error", err)
			}
			if _, writeErr := fmt.Fprintf(stdout, "%s: conversion refused: %s\n", plan.PackagePath, message); writeErr != nil {
				return writeErr
			}
			refused++
			continue
		}
		if !*dry {
			if err := verified.Publish(ctx, conversionPublisher{}); err != nil {
				return err
			}
		}
		for _, model := range plan.Models {
			action := "Converted"
			if *dry {
				action = "Would convert"
			}
			if _, err := fmt.Fprintf(stdout, "%s %s.%s\n", action, plan.PackagePath, model.Name); err != nil {
				return err
			}
			for _, documentation := range model.Documentation {
				action := "moved"
				if documentation.Appended {
					action = "appended below existing field documentation"
				}
				if _, err := fmt.Fprintf(stdout, "  %s.%s: documentation from %s.%s %s\n", model.Name, documentation.Field, model.TableType, documentation.Member, action); err != nil {
					return err
				}
			}
			converted++
		}
	}
	if refused > 0 {
		return safeerror.New(fmt.Sprintf("models convert: %d refusal(s); refused packages were not changed", refused))
	}
	_, err = fmt.Fprintf(stdout, "models convert: %d model(s)\n", converted)
	return err
}

type conversionPublisher struct{}

func (conversionPublisher) Publish(ctx context.Context, dir string, files []modelconvert.File) error {
	return generate.PublishConversion(ctx, dir, conversionFiles(files))
}
func (conversionPublisher) Validate(ctx context.Context, dir string, files []modelconvert.File) error {
	return generate.ValidateConversion(ctx, dir, conversionFiles(files))
}
func conversionFiles(files []modelconvert.File) []generate.ConversionFile {
	replacements := make([]generate.ConversionFile, len(files))
	for i, f := range files {
		replacements[i] = generate.ConversionFile{Name: f.Name, Before: f.Before, After: f.After}
	}
	return replacements
}
