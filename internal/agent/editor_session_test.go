package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Each acceptance function owns one sequential session for its fixture module.
// A probe finishes all of its open/request/close operations before the next
// probe enters. Results and assertions belong to the individual named subtest.
type editorSession struct {
	ctx                   context.Context
	workspace             string
	start                 func(context.Context, string) (*session, error)
	session               *session
	server                ServerInfo
	initializationElapsed time.Duration
	starts                int
}

func newEditorSession(t *testing.T, executable, workspace string) *editorSession {
	t.Helper()
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	// testing cancels t.Context before running cleanup. Keep the process alive
	// for its shutdown handshake, while retaining the overall test deadline.
	ctx := context.WithoutCancel(t.Context())
	var cancel context.CancelFunc
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	h := &editorSession{ctx: ctx, workspace: workspace, start: func(ctx context.Context, workspace string) (*session, error) {
		return start(ctx, executable, []string{"serve"}, workspace)
	}}
	t.Cleanup(func() {
		defer cancel()
		if err := h.close(); err != nil {
			t.Errorf("editor session cleanup: %v", err)
		}
		t.Logf("editor server processes started: %d", h.starts)
	})
	return h
}

func (h *editorSession) close() error {
	if h.session == nil {
		return nil
	}
	s := h.session
	h.session = nil
	return s.close(shutdownGrace)
}

func (h *editorSession) inspect(t *testing.T, options []Options, timeout time.Duration) ([]Result, error) {
	t.Helper()
	if len(options) == 0 || len(options) > MaxBatchOperations || timeout <= 0 {
		return nil, fmt.Errorf("editor scenario requires bounded operations and a positive timeout")
	}
	inspections, err := prepareInspections(h.ctx, options)
	if err != nil {
		return nil, err
	}
	if inspections[0].document.workspace != h.workspace {
		return nil, fmt.Errorf("editor scenario must use its session's fixture workspace")
	}
	var firstErr error
	for attempt := 0; attempt < 2; attempt++ {
		results, responded, err := h.attempt(inspections, timeout)
		if err == nil {
			return results, nil
		}
		// Classify the operation error before intentional cancellation/cleanup
		// adds its own pipe and process-exit errors. Those cannot justify retry.
		retry := h.session != nil && !responded && earlyEditorServerDeath(err) && h.ctx.Err() == nil
		if h.session != nil {
			h.session.cancel() // A failed protocol stream is never reusable.
			err = errors.Join(err, h.close())
		}
		if attempt == 0 && retry {
			firstErr = err
			t.Logf("gopls died before any scenario response; retrying once with a fresh session: %v", err)
			continue
		}
		if firstErr != nil {
			err = fmt.Errorf("editor scenario failed after one early-death retry; first attempt: %w; fresh session: %w", firstErr, err)
		}
		return results, err
	}
	panic("unreachable editor retry")
}

func (h *editorSession) attempt(inspections []preparedInspection, timeout time.Duration) ([]Result, bool, error) {
	if h.session == nil {
		started := time.Now()
		s, err := h.start(h.ctx, h.workspace)
		if err != nil {
			return nil, false, err
		}
		h.starts++
		h.session = s
		err = s.phase(timeout, func() error {
			var err error
			h.server, err = s.initialize(h.workspace)
			return err
		})
		if err != nil {
			return nil, false, err
		}
		h.initializationElapsed = time.Since(started)
	}
	return h.session.inspectPrepared(inspections, h.server, h.initializationElapsed, timeout)
}

func earlyEditorServerDeath(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EPIPE)
}
