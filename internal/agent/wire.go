// Package agent implements read-only language-tooling requests against gopls.
package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxMessageBytes = 16 << 20
const maxHeaderBytes = 8192

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func readMessage(reader *bufio.Reader) (message, error) {
	var result message
	length := -1
	headerBytes := 0
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return result, fmt.Errorf("read LSP header: %w", err)
		}
		headerBytes += len(line)
		if headerBytes > maxHeaderBytes {
			return result, fmt.Errorf("LSP headers exceed limit")
		}
		if !bytes.HasSuffix(line, []byte("\r\n")) {
			return result, fmt.Errorf("LSP header requires CRLF")
		}
		line = line[:len(line)-2]
		if len(line) == 0 {
			break
		}
		for _, b := range line {
			if b > 127 {
				return result, fmt.Errorf("LSP headers must be ASCII")
			}
		}
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return result, fmt.Errorf("malformed LSP header")
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "content-length":
			if length != -1 {
				return result, fmt.Errorf("duplicate LSP Content-Length")
			}
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil || parsed == 0 || parsed > maxMessageBytes {
				return result, fmt.Errorf("invalid or excessive LSP Content-Length")
			}
			length = int(parsed)
		case "content-type":
			_, parameters, err := mime.ParseMediaType(value)
			if err != nil {
				return result, fmt.Errorf("invalid LSP Content-Type")
			}
			charset := strings.ToLower(parameters["charset"])
			if charset != "" && charset != "utf-8" && charset != "utf8" {
				return result, fmt.Errorf("unsupported LSP content encoding")
			}
		}
	}
	if length < 0 {
		return result, fmt.Errorf("LSP message has no Content-Length")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return result, fmt.Errorf("read LSP body: %w", err)
	}
	if !utf8.Valid(data) {
		return result, fmt.Errorf("LSP body is not UTF-8")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode LSP message: %w", err)
	}
	if result.JSONRPC != "2.0" {
		return result, fmt.Errorf("unsupported JSON-RPC version")
	}
	return result, nil
}

func writeMessage(writer io.Writer, value message) error {
	value.JSONRPC = "2.0"
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxMessageBytes {
		return fmt.Errorf("outgoing LSP message exceeds limit")
	}
	var frame bytes.Buffer
	fmt.Fprintf(&frame, "Content-Length: %d\r\n\r\n", len(data))
	frame.Write(data)
	_, err = io.Copy(writer, &frame)
	return err
}

func raw(value any) (json.RawMessage, error) { return json.Marshal(value) }
