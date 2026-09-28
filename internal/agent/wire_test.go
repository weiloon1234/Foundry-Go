package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestWireFramesUseByteLengths(t *testing.T) {
	var stream bytes.Buffer
	for _, method := range []string{"initialize", "🦀"} {
		if err := writeMessage(&stream, message{ID: json.RawMessage("1"), Method: method}); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(&stream)
	for _, want := range []string{"initialize", "🦀"} {
		got, err := readMessage(reader)
		if err != nil {
			t.Fatal(err)
		}
		if got.Method != want || string(got.ID) != "1" {
			t.Fatalf("message = %+v", got)
		}
	}
}

func TestRejectMalformedOrOversizedFrames(t *testing.T) {
	frames := map[string]string{
		"missing length":   "\r\n",
		"duplicate length": "Content-Length: 1\r\nContent-Length: 1\r\n\r\n0",
		"excessive body":   fmt.Sprintf("Content-Length: %d\r\n\r\n", maxMessageBytes+1),
		"negative length":  "Content-Length: -1\r\n\r\n",
		"zero length":      "Content-Length: 0\r\n\r\n",
		"bare newline":     "Content-Length: 1\n\n0",
		"nonascii header":  "X: é\r\nContent-Length: 1\r\n\r\n0",
		"wrong encoding":   "Content-Type: application/vscode-jsonrpc; charset=utf-16\r\nContent-Length: 1\r\n\r\n0",
		"large header":     "X: " + strings.Repeat("x", maxHeaderBytes) + "\r\n\r\n",
		"short body":       "Content-Length: 20\r\n\r\n{}",
		"invalid utf8":     "Content-Length: 1\r\n\r\n\xff",
		"invalid json":     "Content-Length: 1\r\n\r\n!",
		"wrong rpc":        "Content-Length: 2\r\n\r\n{}",
	}
	for name, frame := range frames {
		t.Run(name, func(t *testing.T) {
			if _, err := readMessage(bufio.NewReader(strings.NewReader(frame))); err == nil {
				t.Fatal("accepted invalid frame")
			}
		})
	}
}

func TestWirePropagatesWriteFailure(t *testing.T) {
	if err := writeMessage(failingWriter{}, message{Method: "exit"}); err != io.ErrClosedPipe {
		t.Fatalf("error = %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func FuzzWire(f *testing.F) {
	f.Add([]byte("Content-Length: 17\r\n\r\n{\"jsonrpc\":\"2.0\"}"))
	f.Add([]byte("Content-Length: -1\r\n\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = readMessage(bufio.NewReader(bytes.NewReader(data))) })
}
