package agent

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	return inspectBatch(ctx, options, timeout, executable, []string{"serve"}, shutdownGrace)
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

func inspectBatch(ctx context.Context, options []Options, timeout time.Duration, executable string, args []string, cleanupGrace time.Duration) (results []Result, err error) {
	if ctx == nil || len(options) == 0 || len(options) > MaxBatchOperations || timeout < 0 || cleanupGrace <= 0 {
		return nil, fmt.Errorf("invalid agent inspection batch")
	}
	inspections, err := prepareInspections(ctx, options)
	if err != nil {
		return nil, err
	}
	workspace := inspections[0].document.workspace
	initializationStarted := time.Now()
	session, err := start(ctx, executable, args, workspace)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := session.close(cleanupGrace)
		err = safeerror.Join("operation failed", err, ctx.Err(), closeErr)
	}()
	var server ServerInfo
	err = session.phase(timeout, func() error {
		var failure error
		server, failure = session.initialize(workspace)
		return failure
	})
	if err != nil {
		return nil, err
	}
	results, _, err = session.inspectPrepared(inspections, server, time.Since(initializationStarted), timeout)
	return results, err
}

type preparedInspection struct {
	document  document
	operation string
	method    string
}

func prepareInspections(ctx context.Context, options []Options) ([]preparedInspection, error) {
	options = slices.Clone(options)
	inspections := make([]preparedInspection, len(options))
	for i, item := range options {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		method, err := inspectionMethod(item.Operation)
		if err != nil {
			return nil, err
		}
		doc, err := prepare(item)
		if err != nil {
			return nil, err
		}
		if i != 0 && doc.workspace != inspections[0].document.workspace {
			return nil, fmt.Errorf("agent batch context files must share one consumer workspace")
		}
		inspections[i] = preparedInspection{doc, item.Operation, method}
	}
	return inspections, nil
}

// inspectPrepared shares the production overlay lifecycle with the acceptance
// harness. responded includes a successful request whose didClose later fails;
// such a scenario must never be retried. Session ownership stays with the caller.
func (s *session) inspectPrepared(inspections []preparedInspection, server ServerInfo, initializationElapsed, timeout time.Duration) (results []Result, responded bool, err error) {
	for i, inspection := range inspections {
		doc := inspection.document
		var result Result
		err := s.phase(timeout, func() error {
			s.documentVersion++
			if err := s.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": doc.uri, "languageId": "go", "version": s.documentVersion, "text": doc.text}}); err != nil {
				return err
			}
			requestStarted := time.Now()
			payload, err := s.request(inspection.method, map[string]any{"textDocument": map[string]string{"uri": doc.uri}, "position": doc.position})
			requestElapsed := time.Since(requestStarted)
			if err != nil {
				return err
			}
			responded = true
			if err := s.notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": doc.uri}}); err != nil {
				return err
			}
			result = Result{Operation: inspection.operation, Workspace: doc.workspace, File: doc.path, Position: doc.position, Server: server, Payload: payload,
				Timing: Timing{InitializeNanoseconds: initializationElapsed.Nanoseconds(), RequestNanoseconds: requestElapsed.Nanoseconds()}}
			return nil
		})
		if err != nil {
			return results, responded, fmt.Errorf("agent batch operation %d (%s): %w", i+1, inspection.operation, err)
		}
		results = append(results, result)
	}
	return results, responded, nil
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
		err = safeerror.Join("operation failed", err, ctx.Err())
		cancel()
	}()
	return work()
}
