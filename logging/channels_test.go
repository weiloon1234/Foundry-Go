package logging_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/secret"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelsStackDeduplicatesLeavesAndPreservesPolicy(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")
	settings := map[logging.ChannelName]logging.ChannelSettings{
		"a":       {Sink: logging.SinkConfig{Driver: logging.File, Path: a}},
		"b":       {Sink: logging.SinkConfig{Driver: logging.File, Path: b, Level: slog.LevelWarn}},
		"inner":   {Sink: logging.SinkConfig{Driver: logging.Stack}, Stack: []logging.ChannelName{"a", "b"}},
		"default": {Sink: logging.SinkConfig{Driver: logging.Stack}, Stack: []logging.ChannelName{"inner", "a"}},
	}
	owner, err := logging.PrepareChannels("default", settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings["default"].Stack[0] = "missing"
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("construction opened file")
	}
	logger, err := owner.Channels().Default()
	if err != nil {
		t.Fatal(err)
	}
	alias, _ := owner.Channels().Channel("default")
	if alias != logger {
		t.Fatal("default lost identity")
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	logger.WithGroup("request").With("secret", secret.New("hidden-marker")).Info("once")
	logger.Warn("warning")
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(first), "once") != 1 || strings.Count(string(first), "warning") != 1 || strings.Contains(string(first), "hidden-marker") || !strings.Contains(string(first), "request") {
		t.Fatal("stack duplication/group/redaction failed")
	}
	if strings.Contains(string(second), "once") || strings.Count(string(second), "warning") != 1 {
		t.Fatal("leaf level lost")
	}
	logger.Info("after-close")
	after, _ := os.ReadFile(a)
	if !bytes.Equal(first, after) {
		t.Fatal("closed channel wrote")
	}
}
func TestChannelsValidateGraphAndRollback(t *testing.T) {
	stack := func(names ...logging.ChannelName) logging.ChannelSettings {
		return logging.ChannelSettings{Sink: logging.SinkConfig{Driver: logging.Stack}, Stack: names}
	}
	for _, s := range []map[logging.ChannelName]logging.ChannelSettings{
		{"default": stack("missing")}, {"default": stack("other"), "other": stack("default")}, {"default": stack("a", "a"), "a": logging.DefaultChannelSettings()},
	} {
		if _, err := logging.PrepareChannels("default", s, nil); err == nil {
			t.Fatal("invalid graph accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "a.jsonl")
	owner, err := logging.PrepareChannels("a", map[logging.ChannelName]logging.ChannelSettings{
		"a": {Sink: logging.SinkConfig{Driver: logging.File, Path: path}},
		"z": {Sink: logging.SinkConfig{Driver: logging.File, Path: filepath.Join(t.TempDir(), "missing", "z.jsonl")}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err == nil {
		t.Fatal("missing parent started")
	}
	before, _ := os.ReadFile(path)
	logger, _ := owner.Channels().Default()
	logger.Info("after-rollback")
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("partial startup leaked sink")
	}
	if err := owner.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
func TestBorrowedDefaultLoggerSurvivesOwnedChannels(t *testing.T) {
	var output bytes.Buffer
	borrowed := slog.New(slog.NewJSONHandler(&output, nil))
	owner, err := logging.PrepareChannels("default", map[logging.ChannelName]logging.ChannelSettings{"default": logging.DefaultChannelSettings()}, borrowed)
	if err != nil {
		t.Fatal(err)
	}
	logger, _ := owner.Channels().Default()
	if logger != borrowed {
		t.Fatal("borrowed default duplicated")
	}
	if err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	borrowed.Info("still-alive")
	if !strings.Contains(output.String(), "still-alive") {
		t.Fatal("borrowed logger closed")
	}
}
