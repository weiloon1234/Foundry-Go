package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is a protocol fixture, not evidence of real gopls acceptance.
func TestInspectProtocolAndProcessLifecycle(t *testing.T) {
	for _, operation := range []string{"complete", "hover", "definition"} {
		t.Run(operation, func(t *testing.T) {
			options, source := inspectionFixture(t)
			options.Operation = operation
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "normal"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Server.Name != "protocol-fixture" || result.Position != (Position{1, 6}) {
				t.Fatalf("result = %+v", result)
			}
			if !json.Valid(result.Payload) {
				t.Fatalf("payload = %s", result.Payload)
			}
			data, err := os.ReadFile(options.File)
			if err != nil || string(data) != source {
				t.Fatalf("source was edited: %q, %v", data, err)
			}
			var output bytes.Buffer
			if err := WriteText(&output, result); err != nil || output.Len() == 0 {
				t.Fatalf("text: %q, %v", output.String(), err)
			}
		})
	}
}

func TestInspectErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{{"encoding", "unsupported position encoding"}, {"badid", "unexpected LSP response ID"}, {"rpcerror", "fixture error"}, {"hang", "context deadline exceeded"}, {"malformed", "CRLF"}} {
		t.Run(tc.mode, func(t *testing.T) {
			options, _ := inspectionFixture(t)
			timeout := 3 * time.Second
			if tc.mode == "hang" {
				timeout = 300 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", tc.mode})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if tc.mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestMissingServerDoesNotInstall(t *testing.T) {
	options, _ := inspectionFixture(t)
	options.Gopls = filepath.Join(t.TempDir(), "absent-gopls")
	_, err := Inspect(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "installation is never automatic") {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectAllowsSlowServerShutdown(t *testing.T) {
	options, _ := inspectionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "slow-shutdown"})
	if err != nil || !json.Valid(result.Payload) {
		t.Fatalf("inspection with a slow, responsive shutdown: %s, %v", result.Payload, err)
	}
}

func TestInspectDrainsServerOutputDuringExit(t *testing.T) {
	options, _ := inspectionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "noisy-exit"})
	if err != nil || !json.Valid(result.Payload) {
		t.Fatalf("completed inspection failed while draining server exit output: %s, %v", result.Payload, err)
	}
}

func TestInspectBoundsUnresponsiveShutdown(t *testing.T) {
	for _, mode := range []string{"hang-shutdown", "hang-exit"} {
		t.Run(mode, func(t *testing.T) {
			options, _ := inspectionFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), shutdownGrace+5*time.Second)
			defer cancel()
			started := time.Now()
			result, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", mode})
			if err == nil || !json.Valid(result.Payload) || !strings.Contains(err.Error(), "wait for gopls exit") {
				t.Fatalf("unresponsive cleanup lost its phase or result: %s, %v", result.Payload, err)
			}
			if ctx.Err() != nil || time.Since(started) > shutdownGrace+3*time.Second {
				t.Fatal("cleanup was not independently bounded", ctx.Err())
			}
		})
	}
}

func TestInspectShutdownHonorsCancellation(t *testing.T) {
	options, _ := inspectionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	result, err := inspect(ctx, options, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "hang-shutdown"})
	if !errors.Is(err, context.DeadlineExceeded) || !json.Valid(result.Payload) {
		t.Fatalf("cleanup lost caller cancellation or inspection result: %s, %v", result.Payload, err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancellation waited for the full shutdown grace")
	}
}

func TestTextCompletionListAndArray(t *testing.T) {
	for _, payload := range []string{`{"items":[{"label":"Email","detail":"string"}]}`, `[{"label":"Email","detail":"string"}]`} {
		var output bytes.Buffer
		if err := WriteText(&output, Result{Operation: "complete", Payload: json.RawMessage(payload)}); err != nil {
			t.Fatal(err)
		}
		if output.String() != "Email\tstring\n" {
			t.Fatal(output.String())
		}
	}
	if err := WriteText(failingWriter{}, Result{Operation: "complete", Payload: json.RawMessage(`[{"label":"Email"}]`)}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("error = %v", err)
	}
}

func inspectionFixture(t *testing.T) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.go")
	source := "package models\n// 🦀probe\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return Options{Workspace: dir, File: path, Operation: "complete", Line: 2, Column: 8, Insert: "X"}, source
}

func TestLanguageServerHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	if err := serveFixture(os.Args[len(os.Args)-1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func serveFixture(mode string) error {
	reader := bufio.NewReader(os.Stdin)
	read := func(method string) (message, error) {
		msg, err := readMessage(reader)
		if err == nil && msg.Method != method {
			err = fmt.Errorf("got %s, want %s", msg.Method, method)
		}
		return msg, err
	}
	reply := func(id json.RawMessage, result any) error {
		data, err := raw(result)
		if err != nil {
			return err
		}
		return writeMessage(os.Stdout, message{ID: id, Result: data})
	}
	initialize, err := read("initialize")
	if err != nil {
		return err
	}
	if mode == "hang" {
		time.Sleep(time.Minute)
		return nil
	}
	if mode == "malformed" {
		_, err := fmt.Fprint(os.Stdout, "Content-Length: 1\n\n0")
		return err
	}
	// Ask for configuration and an edit while the client is waiting for its
	// initialize result. It must answer requests and refuse source mutation.
	for _, tc := range []struct{ method, params string }{{"workspace/configuration", `{"items":[{"section":"gopls"}]}`}, {"workspace/applyEdit", `{"edit":{"changes":{}}}`}} {
		if err := writeMessage(os.Stdout, message{ID: json.RawMessage(`"server-1"`), Method: tc.method, Params: json.RawMessage(tc.params)}); err != nil {
			return err
		}
		response, err := readMessage(reader)
		if err != nil {
			return err
		}
		if string(response.ID) != `"server-1"` {
			return fmt.Errorf("server request not answered")
		}
		if tc.method == "workspace/applyEdit" {
			var result struct {
				Applied bool `json:"applied"`
			}
			if err := json.Unmarshal(response.Result, &result); err != nil || result.Applied {
				return fmt.Errorf("client allowed edits")
			}
		}
	}
	encoding := "utf-16"
	if mode == "encoding" {
		encoding = "utf-8"
	}
	if err := reply(initialize.ID, map[string]any{"serverInfo": ServerInfo{Name: "protocol-fixture", Version: "1"}, "capabilities": map[string]string{"positionEncoding": encoding}}); err != nil {
		return err
	}
	if mode == "encoding" {
		return nil
	}
	if _, err := read("initialized"); err != nil {
		return err
	}
	count := 1
	if strings.HasPrefix(mode, "batch") {
		count = 3
	}
	var lastURI string
	for step := range count {
		opened, err := read("textDocument/didOpen")
		if err != nil {
			return err
		}
		var open struct {
			TextDocument struct {
				Text    string `json:"text"`
				URI     string `json:"uri"`
				Version int    `json:"version"`
			} `json:"textDocument"`
		}
		insert := "X"
		if count > 1 {
			insert = []string{"X", "Y", "Z"}[step]
		}
		if err := json.Unmarshal(opened.Params, &open); err != nil || open.TextDocument.Text != "package models\n// 🦀"+insert+"probe\n" || open.TextDocument.Version != step+1 {
			return fmt.Errorf("missing unsaved buffer")
		}
		lastURI = open.TextDocument.URI
		request, err := readMessage(reader)
		if err != nil {
			return err
		}
		var params struct {
			Position     Position `json:"position"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil || params.Position != (Position{1, 6}) || params.TextDocument.URI != open.TextDocument.URI {
			return fmt.Errorf("incorrect request context")
		}
		operationMode := mode
		if mode == "batch-error" && step == 1 {
			operationMode = "rpcerror"
		}
		if mode == "batch-hang" && step == 1 {
			time.Sleep(time.Minute)
		}
		if mode == "batch-slow" {
			time.Sleep(400 * time.Millisecond)
		}
		switch operationMode {
		case "badid":
			if err := reply(json.RawMessage("999"), nil); err != nil {
				return err
			}
		case "rpcerror":
			if err := writeMessage(os.Stdout, message{ID: request.ID, Error: &rpcError{-32001, "fixture error"}}); err != nil {
				return err
			}
		default:
			var payload any
			switch request.Method {
			case "textDocument/completion":
				payload = map[string]any{"items": []map[string]string{{"label": "Email", "detail": "string"}}}
			case "textDocument/hover":
				payload = map[string]any{"contents": map[string]string{"kind": "markdown", "value": "Email string"}}
			case "textDocument/definition":
				payload = []map[string]any{{"uri": open.TextDocument.URI, "range": map[string]any{"start": Position{0, 0}, "end": Position{0, 7}}}}
			default:
				return fmt.Errorf("unexpected operation %s", request.Method)
			}
			if err := reply(request.ID, payload); err != nil {
				return err
			}
			closed, err := read("textDocument/didClose")
			if err != nil {
				return err
			}
			var closing struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if err := json.Unmarshal(closed.Params, &closing); err != nil || closing.TextDocument.URI != open.TextDocument.URI {
				return fmt.Errorf("overlay was not closed")
			}
		}
		if operationMode == "rpcerror" || operationMode == "badid" {
			break
		}
	}
	shutdown, err := read("shutdown")
	if err != nil {
		return err
	}
	if mode == "slow-shutdown" {
		time.Sleep(2100 * time.Millisecond)
	}
	if mode == "hang-shutdown" {
		time.Sleep(time.Minute)
	}
	if err := reply(shutdown.ID, nil); err != nil {
		return err
	}
	_, err = read("exit")
	if mode == "noisy-exit" && err == nil {
		// More than a pipe can retain: exit cannot complete unless the owner keeps
		// consuming final diagnostics after sending the protocol exit notification.
		params, encodeErr := raw(map[string]any{"uri": lastURI, "diagnostics": []map[string]string{{"message": strings.Repeat("x", 64<<10)}}})
		if encodeErr != nil {
			return encodeErr
		}
		for range 32 {
			if err := writeMessage(os.Stdout, message{Method: "textDocument/publishDiagnostics", Params: params}); err != nil {
				return err
			}
		}
	}
	if mode == "hang-exit" && err == nil {
		time.Sleep(time.Minute)
	}
	return err
}
