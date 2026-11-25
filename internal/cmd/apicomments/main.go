// Command apicomments checks that the framework's exported constants, variables
// and caller-input struct fields have doc comments, against the recorded file
// of declarations that still lack one.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weiloon1234/Foundry-Go/internal/apicomment"
	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

func main() {
	flags := flag.NewFlagSet("apicomments", flag.ContinueOnError)
	shrink := flags.Bool("shrink", false, "remove the listed declarations that now have a comment or no longer need one; never adds a line")
	root := flags.String("root", ".", "framework source root")
	if err := docsource.ParseFlags(flags, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}
	if err := run(*root, *shrink); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}
}

func run(root string, shrink bool) error {
	if shrink {
		declarations, err := apicomment.Analyze(root)
		if err != nil {
			return err
		}
		path := filepath.Join(root, apicomment.StagedFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, apicomment.Shrink(declarations, data), 0o644); err != nil {
			return err
		}
	}
	findings, count, err := apicomment.Check(root)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return diagnostic.Join(findings)
	}
	fmt.Println(apicomment.CountText(count))
	return nil
}
