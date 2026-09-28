package logging_test

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/secret"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnedSinkDoesNotOpenUntilStartAndNeverTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("retained\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sink, err := logging.PrepareSink(logging.SinkConfig{Driver: logging.File, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("before")); err == nil {
		t.Fatal("prepared sink accepted write")
	}
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sink.Logger().Info("hello", "credential", secret.New("private-fixture"))
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "retained\n") || strings.Contains(string(data), "private-fixture") || !strings.Contains(string(data), "hello") {
		t.Fatal("append or redaction contract failed")
	}
	if _, err := sink.Write(nil); err == nil {
		t.Fatal("closed sink accepted writes")
	}
}
func TestSinkFailuresAcquireNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "events.jsonl")
	sink, err := logging.PrepareSink(logging.SinkConfig{Driver: logging.File, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sink.Start(ctx); err == nil {
		t.Fatal("cancelled sink opened")
	}
	if err := sink.Start(t.Context()); err == nil {
		t.Fatal("missing parent accepted")
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed sink created path")
	}
	var zero logging.Sink
	if err := zero.Start(t.Context()); err == nil {
		t.Fatal("zero sink started")
	}
}
