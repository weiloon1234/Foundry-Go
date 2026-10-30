package devserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Run starts the development loop:
//  1. Validates options and streams, snapshotting configuration.
//  2. Traps SIGHUP to stop child process groups when the controlling terminal disconnects.
//  3. Starts all configured Helpers concurrently in their own process groups.
//  4. Performs initial compilation of Target via `go build -ldflags=-w -o <Binary> <Target>`,
//     inheriting GOFLAGS from the environment.
//  5. Starts the server child executable in its own process group.
//  6. Enters filesystem watch loop with debounce settling.
//  7. On change: executes OnChange hook, rebuilds binary, and gracefully restarts
//     server child on success while preserving the existing server on build failure.
//  8. On shutdown: drains work, cancels child process groups with SIGTERM, waits up to 5s,
//     and forcefully reaps remaining processes with SIGKILL.
func Run(parent context.Context, opts Options, streams cli.Streams) error {
	if err := streams.Validate(); err != nil {
		return err
	}
	if parent == nil {
		return safeerror.NewFault(fault.Invalid, "devserver requires a context")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	validated, err := opts.Validate()
	if err != nil {
		return err
	}
	opts = validated

	// The children run in their own process groups, so a hangup (the terminal
	// closing) never reaches them: stop them here instead of leaving them to
	// hold their ports. A hangup ignored on purpose (nohup) stays ignored.
	parent, stopHangup := notifyHangup(parent)
	defer stopHangup()

	ctx, stop := context.WithCancelCause(parent)
	defer stop(nil)

	var mu sync.Mutex
	statusWriter := opts.StatusWriter
	if statusWriter == nil {
		statusWriter = streams.Err
	}
	status := newPrefixWriter(&mu, statusWriter, "[dev] ")

	var helpersWG sync.WaitGroup
	for _, helper := range opts.Helpers {
		out := newPrefixWriter(&mu, streams.Err, "["+helper.Name+"] ")
		dir := helper.Dir
		if dir == "" {
			dir = opts.Root
		}
		wait, err := startChild(ctx, dir, helper.Executable, helper.Args, helper.Env, cli.Streams{In: streams.In, Out: out, Err: out})
		if err != nil {
			stop(err)
			break
		}
		helpersWG.Add(1)
		go func(name string) {
			defer helpersWG.Done()
			_ = wait()
			if ctx.Err() == nil {
				stop(safeerror.NewFault(fault.Closed, "the "+name+" helper exited; see its output above"))
			}
		}(helper.Name)
	}

	server := devServer{
		root:    opts.Root,
		binary:  opts.Binary,
		target:  opts.Target,
		args:    opts.Args,
		env:     opts.Env,
		status:  status,
		output:  newPrefixWriter(&mu, streams.Err, "[server] "),
		watcher: newWatcher(opts.Root, opts.Watched),
		opts:    opts,
	}

	if ctx.Err() == nil {
		for _, helper := range opts.Helpers {
			if helper.Banner != "" {
				fmt.Fprintln(status, helper.Banner)
			}
		}
		fmt.Fprintln(status, "press Ctrl+C to stop")
		if err := server.run(ctx); err != nil {
			stop(err)
		}
	}

	stop(nil)
	helpersWG.Wait()

	if err := parent.Err(); err != nil {
		return err
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
}

type devServer struct {
	root    string
	binary  string
	target  string
	args    []string
	env     []string
	status  io.Writer
	output  io.Writer
	watcher *watcher
	opts    Options
	current *runningServer
}

type runningServer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (d *devServer) run(ctx context.Context) error {
	defer d.stop()
	snapshot := d.watcher.snapshot()
	d.rebuild(ctx)
	ticker := time.NewTicker(d.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		next := d.watcher.snapshot()
		if maps.Equal(snapshot, next) {
			continue
		}
		// Let a burst of saves (formatters, generators) settle first.
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(d.opts.SettleInterval):
			}
			settled := d.watcher.snapshot()
			if maps.Equal(next, settled) {
				break
			}
			next = settled
		}
		snapshot = next
		if d.opts.OnChange != nil {
			if err := callback.Isolated("devserver OnChange", d.opts.OnChange); err != nil {
				return err
			}
		}
		fmt.Fprintln(d.status, "change detected; rebuilding")
		d.rebuild(ctx)
	}
}

func (d *devServer) rebuild(ctx context.Context) {
	if err := os.MkdirAll(filepath.Dir(d.binary), 0o755); err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(d.status, "cannot create binary output directory: %s\n", devFailure(err))
		}
		return
	}

	// Link without DWARF is about 3.5x faster (14.2 -> 3.9 s measured); dlv debug builds its own binary.
	build := []string{"build", "-ldflags=-w", "-o", d.binary, d.target}
	if err := runChild(ctx, d.root, "go", build, cli.Streams{Out: d.output, Err: d.output}); err != nil {
		if ctx.Err() == nil {
			next := "fix the error and save a watched file to rebuild"
			if d.running() {
				next = "the previous server keeps running"
			}
			fmt.Fprintf(d.status, "build failed: %s; %s\n", devFailure(err), next)
		}
		return
	}
	d.stop()
	serverCtx, cancel := context.WithCancel(ctx)
	wait, err := startChild(serverCtx, d.root, d.binary, d.args, d.env, cli.Streams{Out: d.output, Err: d.output})
	if err != nil {
		cancel()
		if ctx.Err() == nil {
			fmt.Fprintf(d.status, "cannot start the server binary: %s\n", devFailure(err))
		}
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = wait()
		if serverCtx.Err() == nil {
			fmt.Fprintln(d.status, "server stopped; fix the error above and save a watched file to restart")
		}
	}()
	d.current = &runningServer{cancel: cancel, done: done}
	fmt.Fprintln(d.status, "server started")
}

func (d *devServer) running() bool {
	if d.current == nil {
		return false
	}
	select {
	case <-d.current.done:
		return false
	default:
		return true
	}
}

func (d *devServer) stop() {
	if d.current == nil {
		return
	}
	d.current.cancel()
	<-d.current.done
	d.current = nil
}

func devFailure(err error) string {
	message := err.Error()
	if cause := errors.Unwrap(err); cause != nil {
		message += ": " + cause.Error()
	}
	return message
}

func startChild(ctx context.Context, dir, executable string, args, env []string, streams cli.Streams) (func() error, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Stdin = streams.In
	cmd.Stdout = streams.Out
	cmd.Stderr = streams.Err
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.WaitDelay = DefaultWaitDelay
	finish := configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, safeerror.WrapFault(fault.Missing, "cannot start "+filepath.Base(executable)+"; check that it is installed and the project directory exists", err)
	}
	return func() error {
		err := cmd.Wait()
		finish(ctx.Err() != nil)
		return err
	}, nil
}

func runChild(ctx context.Context, dir, executable string, args []string, streams cli.Streams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wait, err := startChild(ctx, dir, executable, args, nil, streams)
	if err != nil {
		return err
	}
	err = wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type prefixWriter struct {
	mu      *sync.Mutex
	out     io.Writer
	prefix  string
	pending []byte
}

func newPrefixWriter(mu *sync.Mutex, out io.Writer, prefix string) *prefixWriter {
	return &prefixWriter{mu: mu, out: out, prefix: prefix}
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for len(w.pending) > 0 {
		end := bytes.IndexByte(w.pending, '\n') + 1
		if end == 0 {
			if len(w.pending) < 4096 {
				break
			}
			end = len(w.pending)
		}
		line := append([]byte(w.prefix), w.pending[:end]...)
		if line[len(line)-1] != '\n' {
			line = append(line, '\n')
		}
		if _, err := w.out.Write(line); err != nil {
			return 0, err
		}
		w.pending = w.pending[end:]
	}
	return len(p), nil
}
