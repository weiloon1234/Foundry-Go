// Command exportsurface records the framework's exported API and checks that
// every identifier removed since the previous release is in the retired-names
// ledger.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func main() {
	flags := flag.NewFlagSet("exportsurface", flag.ContinueOnError)
	check := flags.Bool("check", false, "compare without writing")
	release := flags.String("release", "", "record the current inventory and ledger as the release `tag`")
	root := flags.String("root", ".", "framework source root")
	out := flags.String("out", "", "write the computed inventory here instead of the recorded file (for a checkout of an earlier tag)")
	if err := docsource.ParseFlags(flags, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}
	if err := run(*root, *out, *check, *release); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}
}

func run(root, out string, check bool, release string) error {
	current, err := retirement.Surface(root)
	if err != nil {
		return err
	}
	if out != "" {
		return os.WriteFile(out, current, 0o644)
	}
	if release != "" {
		ledger, err := retirement.LedgerNames()
		if err != nil {
			return err
		}
		if err := write(root, retirement.SurfaceFile, current); err != nil {
			return err
		}
		if err := write(root, retirement.ReleaseSurfaceFile, retirement.ReleaseSurface(current, release)); err != nil {
			return err
		}
		return write(root, retirement.ReleaseLedgerFile, ledger)
	}
	recorded, err := os.ReadFile(filepath.Join(root, retirement.SurfaceFile))
	if check {
		if err != nil || !bytes.Equal(recorded, current) {
			return safeerror.New(retirement.SurfaceFile + ":0: the recorded exported API is stale; run go run ./internal/cmd/exportsurface and review the diff")
		}
	} else if err := write(root, retirement.SurfaceFile, current); err != nil {
		return err
	}
	return retirement.CheckRemovals(root)
}

func write(root, name string, data []byte) error {
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
