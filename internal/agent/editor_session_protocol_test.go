package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func editorSessionFixture(t *testing.T, workspace string, modes ...string) *editorSession {
	t.Helper()
	h := newEditorSession(t, os.Args[0], workspace)
	h.start = func(ctx context.Context, workspace string) (*session, error) {
		mode := modes[min(h.starts, len(modes)-1)]
		return start(ctx, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", mode}, workspace)
	}
	return h
}

func TestEditorSessionReusesProcessAndResetsOverlays(t *testing.T) {
	options, source := batchFixture(t)
	path, err := filepath.EvalSymlinks(options[0].File)
	if err != nil {
		t.Fatal(err)
	}
	h := editorSessionFixture(t, options[0].Workspace, "editor-normal")
	// Use separate scenarios on the same URI, including reopening the disk
	// contents after unsaved edits. The fixture rejects any missing didClose.
	options = append(options, options[0])
	options[3].Insert = ""
	for _, option := range options {
		t.Run(option.Operation+option.Insert, func(t *testing.T) {
			results, err := h.inspect(t, []Options{option}, 3*time.Second)
			if err != nil || len(results) != 1 {
				t.Fatalf("inspect: %d results, %v", len(results), err)
			}
			var payload struct{ Text, URI string }
			if err := json.Unmarshal(results[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(source, "probe", option.Insert+"probe", 1)
			if payload.Text != want || payload.URI != fileURI(path) {
				t.Fatalf("overlay or diagnostic leaked into response: %+v", payload)
			}
		})
	}
	if h.starts != 1 {
		t.Fatalf("started %d servers for one workspace", h.starts)
	}
	if err := h.close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(options[0].File); err != nil || string(data) != source {
		t.Fatal("editor scenarios edited source", err)
	}
}

func TestEditorSessionRetriesEarlyDeathOnce(t *testing.T) {
	for _, mode := range []string{"editor-die-initialize", "editor-die-before-response", "editor-close-pipe-before-response"} {
		t.Run(mode, func(t *testing.T) {
			options, _ := batchFixture(t)
			h := editorSessionFixture(t, options[0].Workspace, mode, "editor-normal")
			results, err := h.inspect(t, options, 3*time.Second)
			if err != nil || len(results) != len(options) || h.starts != 2 {
				t.Fatalf("early death did not recover exactly once: %d results, %d starts, %v", len(results), h.starts, err)
			}
			// The replacement stays healthy and reusable for the next scenario.
			if _, err := h.inspect(t, options[:1], 3*time.Second); err != nil || h.starts != 2 {
				t.Fatal("replacement was not reusable", h.starts, err)
			}
		})
	}
}

func TestEditorSessionSecondDeathDoesNotPoisonLaterScenarios(t *testing.T) {
	options, _ := batchFixture(t)
	h := editorSessionFixture(t, options[0].Workspace, "editor-die-initialize", "editor-die-before-response", "editor-normal")
	results, err := h.inspect(t, options, 3*time.Second)
	if err == nil || len(results) != 0 || h.starts != 2 || h.session != nil ||
		!strings.Contains(err.Error(), "failed after one early-death retry") ||
		!strings.Contains(err.Error(), "first attempt:") || !strings.Contains(err.Error(), "fresh session:") {
		t.Fatalf("second death was hidden or session retained: %d results, %d starts, %v", len(results), h.starts, err)
	}
	results, err = h.inspect(t, options, 3*time.Second)
	if err != nil || len(results) != len(options) || h.starts != 3 {
		t.Fatalf("failure cascaded into next scenario: %d results, %d starts, %v", len(results), h.starts, err)
	}
}

func TestEditorSessionRetriesDeathInReusedSession(t *testing.T) {
	option, _ := inspectionFixture(t)
	h := editorSessionFixture(t, option.Workspace, "editor-die-after-response", "editor-normal")
	for scenario := range 2 {
		results, err := h.inspect(t, []Options{option}, 3*time.Second)
		if err != nil || len(results) != 1 || h.starts != scenario+1 {
			t.Fatalf("scenario %d did not recover from idle server death: %d results, %d starts, %v", scenario, len(results), h.starts, err)
		}
	}
}

func TestEditorSessionDoesNotRetryProtocolErrorsOrResponses(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		completed int
		want      string
	}{
		{"malformed", 0, "CRLF"},
		{"encoding", 0, "unsupported position encoding"},
		{"badid", 0, "unexpected LSP response ID"},
		{"rpcerror", 0, "fixture error"},
		{"editor-malformed-response", 0, "decode LSP message"},
		{"editor-die-after-response", 1, "EOF"},
		{"editor-close-pipe-after-response", 0, "broken pipe"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			options, _ := batchFixture(t)
			h := editorSessionFixture(t, options[0].Workspace, tc.mode, "editor-normal")
			results, err := h.inspect(t, options, 3*time.Second)
			if err == nil || len(results) != tc.completed || h.starts != 1 || h.session != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("failure was retried or lost: %d results, %d starts, %v", len(results), h.starts, err)
			}
			if _, err := h.inspect(t, options, 3*time.Second); err != nil || h.starts != 2 {
				t.Fatal("failed protocol stream was reused", h.starts, err)
			}
		})
	}
}

func TestEditorSessionDeadlinesDiscardWithoutRetry(t *testing.T) {
	for _, mode := range []string{"hang", "editor-hang-before-response", "editor-hang-after-response"} {
		t.Run(mode, func(t *testing.T) {
			options, _ := batchFixture(t)
			h := editorSessionFixture(t, options[0].Workspace, mode, "editor-normal")
			started := time.Now()
			results, err := h.inspect(t, options, 500*time.Millisecond)
			completed := 0
			if mode == "editor-hang-after-response" {
				completed = 1
			}
			if !errors.Is(err, context.DeadlineExceeded) || len(results) != completed || h.starts != 1 || h.session != nil || time.Since(started) > 3*time.Second {
				t.Fatalf("deadline was lost or retried: %d results, %d starts, %v", len(results), h.starts, err)
			}
			if _, err := h.inspect(t, options, 3*time.Second); err != nil || h.starts != 2 {
				t.Fatal("timed-out session was reused", h.starts, err)
			}
		})
	}
}

func TestEditorSessionReturnsAssertionPayloadWithoutRetry(t *testing.T) {
	option, _ := inspectionFixture(t)
	h := editorSessionFixture(t, option.Workspace, "editor-normal")
	results, err := h.inspect(t, []Options{option}, 3*time.Second)
	if err != nil || len(results) != 1 || h.starts != 1 {
		t.Fatal("inspection failed", len(results), h.starts, err)
	}
	// This fixture intentionally does not return completion items. The harness
	// returns that mismatch to the probe's assertion instead of trying again.
	var payload struct{ Items []json.RawMessage }
	if err := json.Unmarshal(results[0].Payload, &payload); err != nil || len(payload.Items) != 0 {
		t.Fatal("fixture did not return the expected assertion mismatch", err)
	}
}

func TestEditorSessionReportsFinalShutdownFailure(t *testing.T) {
	option, _ := inspectionFixture(t)
	h := editorSessionFixture(t, option.Workspace, "editor-shutdown-error")
	if _, err := h.inspect(t, []Options{option}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.close(); err == nil || !strings.Contains(err.Error(), "gopls shutdown request") || h.starts != 1 {
		t.Fatal("shutdown failure was hidden or retried", h.starts, err)
	}
	if err := h.close(); err != nil {
		t.Fatal("closed session was retained", err)
	}
}

func TestEditorSessionRejectsOtherWorkspacesBeforeStarting(t *testing.T) {
	option, _ := inspectionFixture(t)
	other, _ := inspectionFixture(t)
	h := editorSessionFixture(t, option.Workspace, "editor-normal")
	if _, err := h.inspect(t, []Options{other}, time.Second); err == nil || h.starts != 0 {
		t.Fatal("scenario crossed fixture workspaces", h.starts, err)
	}
}

func TestEarlyEditorDeathClassification(t *testing.T) {
	for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, os.ErrClosed, syscall.EPIPE} {
		wrapped := fmt.Errorf("operation: %w", cause)
		if !earlyEditorServerDeath(wrapped) {
			t.Fatal("lost closed pipe", cause)
		}
		for _, deadline := range []error{context.Canceled, context.DeadlineExceeded} {
			if earlyEditorServerDeath(errors.Join(wrapped, deadline)) {
				t.Fatal("deadline-induced pipe closure became retryable", cause, deadline)
			}
		}
	}
	if earlyEditorServerDeath(errors.New("malformed protocol")) {
		t.Fatal("protocol failure became retryable")
	}
}

// The shared fixture has already initialized this connection. Echo overlays and
// enforce close-before-open, including increasing versions across scenarios.
func serveEditorSessionFixture(reader *bufio.Reader, mode string) error {
	reply := func(id json.RawMessage, value any) error {
		payload, err := raw(value)
		if err != nil {
			return err
		}
		return writeMessage(os.Stdout, message{ID: id, Result: payload})
	}
	var uri, text string
	version, requests := 0, 0
	for {
		msg, err := readMessage(reader)
		if err != nil {
			return err
		}
		switch msg.Method {
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI, Text string
					Version   int
				}
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return err
			}
			if uri != "" || params.TextDocument.Version <= version {
				return fmt.Errorf("overlay leaked or version did not advance")
			}
			uri, text, version = params.TextDocument.URI, params.TextDocument.Text, params.TextDocument.Version
		case "textDocument/completion", "textDocument/hover", "textDocument/definition":
			var params struct{ TextDocument struct{ URI string } }
			if err := json.Unmarshal(msg.Params, &params); err != nil || uri == "" || params.TextDocument.URI != uri {
				return fmt.Errorf("request used a closed or different document")
			}
			requests++
			if mode == "editor-die-before-response" || (mode == "editor-die-after-response" && requests == 2) {
				return fmt.Errorf("fixture exited during inspection")
			}
			if mode == "editor-close-pipe-before-response" {
				os.Stdout.Close()
				time.Sleep(time.Minute)
				return nil
			}
			if mode == "editor-hang-before-response" || (mode == "editor-hang-after-response" && requests == 2) {
				time.Sleep(time.Minute)
				return nil
			}
			if mode == "editor-malformed-response" {
				_, err := io.WriteString(os.Stdout, "Content-Length: 1\r\n\r\n{")
				return err
			}
			if mode == "editor-close-pipe-after-response" {
				os.Stdin.Close()
			}
			if err := reply(msg.ID, map[string]string{"text": text, "uri": uri}); err != nil {
				return err
			}
			if mode == "editor-close-pipe-after-response" {
				time.Sleep(time.Minute)
				return nil
			}
		case "textDocument/didClose":
			var params struct{ TextDocument struct{ URI string } }
			if err := json.Unmarshal(msg.Params, &params); err != nil || uri == "" || params.TextDocument.URI != uri {
				return fmt.Errorf("closed wrong overlay")
			}
			// Deliberately queue stale diagnostics after closing. They must not
			// be returned as the next scenario's response or retained as state.
			paramsJSON, err := raw(map[string]any{"uri": uri, "version": version, "diagnostics": []map[string]string{{"message": text}}})
			if err != nil {
				return err
			}
			if err := writeMessage(os.Stdout, message{Method: "textDocument/publishDiagnostics", Params: paramsJSON}); err != nil {
				return err
			}
			uri, text = "", ""
		case "shutdown":
			if uri != "" {
				return fmt.Errorf("shutdown retained an overlay")
			}
			if mode == "editor-shutdown-error" {
				return writeMessage(os.Stdout, message{ID: msg.ID, Error: &rpcError{-32001, "fixture shutdown error"}})
			}
			if err := reply(msg.ID, nil); err != nil {
				return err
			}
		case "exit":
			return nil
		default:
			return fmt.Errorf("unexpected editor request %s", msg.Method)
		}
	}
}
