package main

import (
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/doctor"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const deployUsage = "usage: foundry deploy [--dir application] [--go go] [--python python3] bootstrap|deploy|release|help [--environment name] [--action name] ..."

type deployOptions struct {
	dir, goBinary, python string
	arguments             []string
	build                 frameworkinfo.Build
}

func runDeploy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if handled, err := subcommandHelp(args, stdout, deployUsage); handled {
		return err
	}
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := deployOptions{build: frameworkinfo.CurrentBuild()}
	flags.StringVar(&options.dir, "dir", ".", "application module directory")
	flags.StringVar(&options.goBinary, "go", "go", "installed Go executable")
	flags.StringVar(&options.python, "python", "python3", "installed Python executable")
	if err := cli.ParseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return clidiag.Usage(deployUsage)
	}
	options.arguments = flags.Args()
	path, argv, err := deployCommand(ctx, options)
	if err != nil {
		return err
	}
	// Replacing this process keeps the terminal for hidden prompts and makes Python's exit status the command's.
	syscall.Exec(path, argv, os.Environ())
	return safeerror.New("Python could not start the pinned deploy tool")
}

func deployCommand(ctx context.Context, options deployOptions) (path string, argv []string, err error) {
	dir, err := filepath.Abs(options.dir)
	if err != nil {
		return "", nil, err
	}
	framework, err := doctor.SelectedFramework(ctx, doctor.Options{Dir: dir, Go: options.goBinary, Timeout: 30 * time.Second})
	if err != nil {
		return "", nil, err
	}
	if _, err := options.build.Check(framework.Selection); err != nil {
		return "", nil, err
	}
	entry := filepath.Join(framework.Dir, frameworkinfo.DeployEntry)
	if _, err := os.Stat(entry); err != nil {
		return "", nil, safeerror.New("The selected Foundry-Go has no deploy tool; update the application's framework requirement")
	}
	path, err = exec.LookPath(options.python)
	if err != nil {
		return "", nil, safeerror.New("Python was not found; install it or select an executable with --python")
	}
	argv = append([]string{options.python, "-B", entry, "--app-root", dir}, options.arguments...)
	return path, argv, nil
}
