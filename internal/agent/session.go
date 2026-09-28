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
	"os/exec"
	"strconv"
	"time"
)

// A cold workspace may still be canceling background analysis when shutdown
// arrives. Allow it to finish while bounding cleanup independently of requests.
const shutdownGrace = 5 * time.Second

type session struct {
	ctx         context.Context
	cancel      context.CancelFunc
	reader      *bufio.Reader
	output      *os.File
	writer      io.WriteCloser
	stopRead    func() bool
	done        chan struct{}
	exitErr     error
	nextID      int
	initialized bool
}

func start(ctx context.Context, executable string, args []string, workspace string) (*session, error) {
	child, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(child, executable, args...)
	cmd.Dir = workspace
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Own stdout separately: Cmd.Wait must not close a StdoutPipe while the
	// protocol reader is still consuming a server's final response.
	output, serverOutput, err := os.Pipe()
	if err != nil {
		cancel()
		input.Close()
		return nil, err
	}
	cmd.Stdout = serverOutput
	if err := cmd.Start(); err != nil {
		cancel()
		input.Close()
		output.Close()
		serverOutput.Close()
		return nil, fmt.Errorf("start gopls (installation is never automatic): %w", err)
	}
	serverOutput.Close()
	s := &session{ctx: child, cancel: cancel, reader: bufio.NewReaderSize(output, maxHeaderBytes), output: output, writer: input, done: make(chan struct{})}
	// Cancellation closes both owned protocol pipes, even if a subprocess
	// inherited a server pipe. The parent never signals unrelated processes.
	s.stopRead = context.AfterFunc(child, func() { output.Close(); input.Close() })
	go func() { s.exitErr = cmd.Wait(); close(s.done) }()
	return s, nil
}

// request is sequential: each inspection scenario owns one server session. Incoming
// server requests are answered while waiting; read-only clients never apply edits.
func (s *session) request(method string, params any) (json.RawMessage, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := raw(params)
	if err != nil {
		return nil, err
	}
	s.nextID++
	id := json.RawMessage(strconv.Itoa(s.nextID))
	if err := writeMessage(s.writer, message{ID: id, Method: method, Params: encoded}); err != nil {
		return nil, err
	}
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		response, err := readMessage(s.reader)
		if err != nil {
			if s.ctx.Err() != nil {
				return nil, s.ctx.Err()
			}
			return nil, err
		}
		if response.Method != "" {
			if len(response.ID) != 0 && !bytes.Equal(response.ID, []byte("null")) {
				if err := s.answer(response); err != nil {
					return nil, err
				}
			}
			continue
		}
		if !bytes.Equal(response.ID, id) {
			return nil, fmt.Errorf("unexpected LSP response ID")
		}
		if response.Error != nil {
			return nil, fmt.Errorf("gopls %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
		}
		if len(response.Result) == 0 {
			return nil, fmt.Errorf("LSP response is missing its result")
		}
		return response.Result, nil
	}
}
func (s *session) notify(method string, params any) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	encoded, err := raw(params)
	if err != nil {
		return err
	}
	return writeMessage(s.writer, message{Method: method, Params: encoded})
}
func (s *session) answer(request message) error {
	var result any
	switch request.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return err
		}
		values := make([]map[string]any, len(params.Items))
		for i := range values {
			values[i] = map[string]any{}
		}
		result = values
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability":
		result = nil
	case "workspace/applyEdit":
		result = struct {
			Applied bool   `json:"applied"`
			Reason  string `json:"failureReason"`
		}{false, "Foundry agent requests are read-only"}
	default:
		return writeMessage(s.writer, message{ID: request.ID, Error: &rpcError{-32601, "method is not supported by this read-only client"}})
	}
	encoded, err := raw(result)
	if err != nil {
		return err
	}
	return writeMessage(s.writer, message{ID: request.ID, Result: encoded})
}

func (s *session) close() error {
	// Shutdown itself is bounded even if gopls stops answering. Cancellation kills
	// only this owned process; WaitDelay bounds inherited-pipe cleanup afterward.
	timer := time.AfterFunc(shutdownGrace, s.cancel)
	defer timer.Stop()
	var shutdownErr error
	if s.initialized && s.ctx.Err() == nil {
		if _, err := s.request("shutdown", nil); err != nil {
			shutdownErr = fmt.Errorf("gopls shutdown request: %w", err)
		} else if err := s.notify("exit", nil); err != nil {
			shutdownErr = fmt.Errorf("gopls exit notification: %w", err)
		}
	}
	_ = s.writer.Close()
	if !s.initialized || shutdownErr != nil {
		s.cancel()
	}
	// Final diagnostics may still be queued after the shutdown response. Keep
	// draining stdout while waiting for exit: a full pipe must not prevent this
	// responsive server from finishing. No protocol request remains in flight.
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, s.reader)
		close(drained)
	}()
	<-s.done
	s.stopRead()
	s.output.Close() // Also releases a drain if another process retained stdout.
	<-drained
	s.cancel()
	var exitErr error
	if s.exitErr != nil {
		exitErr = fmt.Errorf("wait for gopls exit: %w", s.exitErr)
	}
	return errors.Join(shutdownErr, exitErr)
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

func (s *session) initialize(workspace string) (ServerInfo, error) {
	params := map[string]any{
		"processId": os.Getpid(), "rootUri": fileURI(workspace),
		"clientInfo":       map[string]string{"name": "Foundry-Go agent"},
		"workspaceFolders": []map[string]string{{"uri": fileURI(workspace), "name": "consumer"}},
		"capabilities": map[string]any{
			"general":   map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace": map[string]any{"configuration": true, "applyEdit": false, "workspaceFolders": true},
			"textDocument": map[string]any{
				"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": false, "documentationFormat": []string{"markdown", "plaintext"}}},
				"hover":      map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition": map[string]any{"linkSupport": true},
			},
		},
	}
	result, err := s.request("initialize", params)
	if err != nil {
		return ServerInfo{}, err
	}
	var initialized struct {
		ServerInfo   ServerInfo `json:"serverInfo"`
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initialized); err != nil {
		return ServerInfo{}, err
	}
	if encoding := initialized.Capabilities.PositionEncoding; encoding != "" && encoding != "utf-16" {
		return ServerInfo{}, fmt.Errorf("gopls selected unsupported position encoding %s", encoding)
	}
	s.initialized = true
	if err := s.notify("initialized", struct{}{}); err != nil {
		return ServerInfo{}, err
	}
	return initialized.ServerInfo, nil
}
