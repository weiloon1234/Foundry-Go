package devserver

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Default timing intervals for filesystem monitoring and child process lifecycle.
const (
	DefaultPollInterval   = 500 * time.Millisecond
	DefaultSettleInterval = 200 * time.Millisecond
	DefaultWaitDelay      = 5 * time.Second
)

// Helper specifies a persistent auxiliary background process to supervise,
// such as a frontend Vite portal dev server.
type Helper struct {
	// Name identifies the helper process and prefixes its output lines (e.g. "admin" -> "[admin] ").
	Name string

	// Executable is the binary or script to run (e.g. "npm").
	Executable string

	// Args contains command line arguments (e.g. ["--prefix", "web", "run", "dev", ...]).
	Args []string

	// Dir is the working directory for the command. If empty, defaults to Options.Root.
	Dir string

	// Env contains optional additional environment variables.
	Env []string

	// Banner is an optional startup announcement line printed to the status stream
	// when all children are ready (e.g. "admin: http://127.0.0.1:8080/ (Vite on 5173)").
	Banner string
}

// Options configures the development server supervision loop.
type Options struct {
	// Root is the project root directory. Required.
	Root string

	// Target is the Go package to build (default: "./cmd/artisan").
	Target string

	// Binary is the path where the built executable is placed.
	// Optional; defaults to filepath.Join(Root, "bin", "dev-server").
	Binary string

	// Args contains arguments passed to the server binary (e.g. ["serve", ...]).
	Args []string

	// Env contains environment variables added to the server child (e.g. ["WEB__ENABLED=true", ...]).
	Env []string

	// Watched contains additional file, directory, or glob paths to watch beyond the defaults.
	// Default watched paths include cmd/, internal/, root .go files, go.mod, and go.sum.
	// Glob patterns and relative paths are evaluated relative to Root.
	Watched []string

	// PollInterval is the polling frequency for filesystem changes (default: 500ms).
	PollInterval time.Duration

	// SettleInterval is the debounce duration after a detected change (default: 200ms).
	SettleInterval time.Duration

	// Helpers defines persistent child processes to run alongside the server
	// (e.g. Vite dev servers for frontend portals).
	Helpers []Helper

	// OnChange is an optional validation callback executed after a file change settles,
	// before rebuilding the server binary. If it returns an error or panics, the loop
	// halts cleanly and returns a safe framework error.
	OnChange func() error

	// StatusWriter optionally overrides where dev lifecycle status messages are written.
	// If nil, defaults to Streams.Err.
	StatusWriter io.Writer
}

// Validate verifies configuration options and returns a normalized, snapshotted copy.
func (o Options) Validate() (Options, error) {
	if o.Root == "" {
		return Options{}, safeerror.NewFault(fault.Invalid, "devserver requires a project root directory")
	}
	abs, err := filepath.Abs(o.Root)
	if err != nil {
		return Options{}, safeerror.WrapFault(fault.Invalid, "cannot resolve project directory", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Options{}, safeerror.WrapFault(fault.Invalid, "project directory does not exist", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return Options{}, safeerror.NewFault(fault.Invalid, "project directory does not exist")
	}

	res := o
	res.Root = resolved
	if res.Target == "" {
		res.Target = "./cmd/artisan"
	}
	if res.Binary == "" {
		res.Binary = filepath.Join(res.Root, "bin", "dev-server")
	}
	if res.PollInterval <= 0 {
		res.PollInterval = DefaultPollInterval
	}
	if res.SettleInterval <= 0 {
		res.SettleInterval = DefaultSettleInterval
	}
	res.Args = slices.Clone(o.Args)
	res.Env = slices.Clone(o.Env)
	res.Watched = slices.Clone(o.Watched)

	if len(o.Helpers) > 0 {
		res.Helpers = make([]Helper, len(o.Helpers))
		for i, h := range o.Helpers {
			if h.Name == "" || h.Executable == "" {
				return Options{}, safeerror.NewFault(fault.Invalid, "helper requires name and executable")
			}
			clone := h
			clone.Args = slices.Clone(h.Args)
			clone.Env = slices.Clone(h.Env)
			res.Helpers[i] = clone
		}
	}
	return res, nil
}
