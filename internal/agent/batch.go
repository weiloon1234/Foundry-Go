package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// MaxBatchOperations bounds prepared source overlays retained by one session.
const MaxBatchOperations = 16

// InspectBatch owns one fresh language-server session for a related scenario.
// Each operation opens its independently prepared overlay and closes it before
// the next operation. Initialization and each operation have separate deadlines;
// any failure stops the batch and closes the owned process. Returned results are
// the completed prefix; callers must still check the error, including cleanup.
// No server or unsaved state is shared between calls.
func InspectBatch(ctx context.Context, options []Options, timeout time.Duration) ([]Result, error) {
	if len(options) == 0 || len(options) > MaxBatchOperations || timeout <= 0 {
		return nil, fmt.Errorf("agent batch requires bounded operations and a positive operation timeout")
	}
	executable := options[0].Gopls
	if executable == "" {
		executable = "gopls"
	}
	for _, item := range options {
		selected := item.Gopls
		if selected == "" {
			selected = "gopls"
		}
		if selected != executable {
			return nil, fmt.Errorf("agent batch must use one language-server executable")
		}
	}
	return inspectBatch(ctx, options, timeout, executable, []string{"serve"})
}

func inspectionMethod(operation string) (string, error) {
	switch operation {
	case "complete":
		return "textDocument/completion", nil
	case "hover":
		return "textDocument/hover", nil
	case "definition":
		return "textDocument/definition", nil
	default:
		return "", fmt.Errorf("agent operation must be complete, hover, or definition")
	}
}

func inspectBatch(ctx context.Context, options []Options, timeout time.Duration, executable string, args []string) (results []Result, err error) {
	if ctx == nil || len(options) == 0 || len(options) > MaxBatchOperations || timeout < 0 {
		return nil, fmt.Errorf("invalid agent inspection batch")
	}
	options = slices.Clone(options)
	documents := make([]document, len(options))
	methods := make([]string, len(options))
	for i, item := range options {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		methods[i], err = inspectionMethod(item.Operation)
		if err != nil {
			return nil, err
		}
		documents[i], err = prepare(item)
		if err != nil {
			return nil, err
		}
		if i != 0 && documents[i].workspace != documents[0].workspace {
			return nil, fmt.Errorf("agent batch context files must share one consumer workspace")
		}
	}
	initializationStarted := time.Now()
	session, err := start(ctx, executable, args, documents[0].workspace)
	if err != nil {
		return nil, err
	}
	defer func() { closeErr := session.close(); err = errors.Join(err, ctx.Err(), closeErr) }()
	var server ServerInfo
	err = session.phase(timeout, func() error {
		var failure error
		server, failure = session.initialize(documents[0].workspace)
		return failure
	})
	if err != nil {
		return nil, err
	}
	initializationElapsed := time.Since(initializationStarted)
	for i, doc := range documents {
		var result Result
		err := session.phase(timeout, func() error {
			if err := session.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": doc.uri, "languageId": "go", "version": i + 1, "text": doc.text}}); err != nil {
				return err
			}
			requestStarted := time.Now()
			payload, err := session.request(methods[i], map[string]any{"textDocument": map[string]string{"uri": doc.uri}, "position": doc.position})
			requestElapsed := time.Since(requestStarted)
			if err != nil {
				return err
			}
			if err := session.notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": doc.uri}}); err != nil {
				return err
			}
			result = Result{Operation: options[i].Operation, Workspace: doc.workspace, File: doc.path, Position: doc.position, Server: server, Payload: payload,
				Timing: Timing{InitializeNanoseconds: initializationElapsed.Nanoseconds(), RequestNanoseconds: requestElapsed.Nanoseconds()}}
			return nil
		})
		if err != nil {
			return results, fmt.Errorf("agent batch operation %d (%s): %w", i+1, options[i].Operation, err)
		}
		results = append(results, result)
	}
	return results, nil
}

// A timed-out phase cancels this owned session: its protocol stream is no
// longer reusable. Synchronize cancellation before returning so a racing timer
// cannot kill a successfully completed operation's successor.
func (s *session) phase(timeout time.Duration, work func() error) (err error) {
	if timeout == 0 {
		return work()
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { s.cancel(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		err = errors.Join(err, ctx.Err())
		cancel()
	}()
	return work()
}
