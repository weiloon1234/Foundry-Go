package logging_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/logging"
)

type failingHandler struct{ slog.Handler }

func (failingHandler) Handle(context.Context, slog.Record) error { return errors.New("collector down") }

func TestStackKeepsDeliveringWhenOneChildFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	owner, err := logging.PrepareChannels("default", map[logging.ChannelName]logging.ChannelSettings{
		"file":    {Sink: logging.SinkConfig{Driver: logging.File, Path: path}},
		"default": {Sink: logging.SinkConfig{Driver: logging.Stack}, Stack: []logging.ChannelName{"remote", "file"}},
	}, nil, logging.WithHandler("remote", failingHandler{slog.NewJSONHandler(&bytes.Buffer{}, nil)}))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	logger, _ := owner.Channels().Default()
	if err := logger.Handler().Handle(t.Context(), slog.NewRecord(nowForTest(), slog.LevelInfo, "delivered", 0)); err == nil {
		t.Fatal("stack hid its failing child")
	}
	logger.Info("second")
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "delivered") || !strings.Contains(string(data), "second") {
		t.Fatal("failing child suppressed another stack child", err)
	}
	stats := owner.Stats()
	if len(stats) != 2 || stats[0].Channel != "file" || stats[0].Sink.Records != 2 || stats[1].Channel != "remote" || stats[1].Sink.Driver != logging.Custom || stats[1].Sink.Failures != 2 || stats[1].Sink.Dropped != 2 {
		t.Fatal("channel statistics are wrong", stats)
	}
}

func TestCustomHandlerChannelsApplyLevelAndValidateBinding(t *testing.T) {
	var output bytes.Buffer
	handler := slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})
	owner, err := logging.PrepareChannels("default", map[logging.ChannelName]logging.ChannelSettings{
		"default": logging.DefaultChannelSettings(),
		"audit":   {Sink: logging.SinkConfig{Driver: logging.Custom, Level: slog.LevelWarn}},
	}, nil, logging.WithHandler("audit", handler))
	if err != nil {
		t.Fatal(err)
	}
	audit, err := owner.Channels().Channel("audit")
	if err != nil {
		t.Fatal(err)
	}
	audit.Info("filtered")
	audit.With("tenant", "t1").WithGroup("request").Warn("kept", "id", 7)
	if strings.Contains(output.String(), "filtered") || !strings.Contains(output.String(), `"tenant":"t1"`) || !strings.Contains(output.String(), `"request":{"id":7}`) {
		t.Fatal("custom channel level or handler derivation failed", output.String())
	}
	for name, test := range map[string]struct {
		settings map[logging.ChannelName]logging.ChannelSettings
		options  []logging.ChannelOption
		code     fault.Code
	}{
		"handler-for-sink":   {map[logging.ChannelName]logging.ChannelSettings{"default": logging.DefaultChannelSettings()}, []logging.ChannelOption{logging.WithHandler("default", handler)}, fault.Invalid},
		"custom-no-handler":  {map[logging.ChannelName]logging.ChannelSettings{"default": {Sink: logging.SinkConfig{Driver: logging.Custom}}}, nil, fault.Missing},
		"nil-handler":        {map[logging.ChannelName]logging.ChannelSettings{"default": logging.DefaultChannelSettings()}, []logging.ChannelOption{logging.WithHandler("x", nil)}, fault.Invalid},
		"duplicate-handler":  {map[logging.ChannelName]logging.ChannelSettings{"default": logging.DefaultChannelSettings()}, []logging.ChannelOption{logging.WithHandler("x", handler), logging.WithHandler("x", handler)}, fault.Duplicate},
		"custom-file-option": {map[logging.ChannelName]logging.ChannelSettings{"default": {Sink: logging.SinkConfig{Driver: logging.Custom, Path: "/tmp/x"}}}, []logging.ChannelOption{logging.WithHandler("default", handler)}, fault.Invalid},
	} {
		if _, err := logging.PrepareChannels("default", test.settings, nil, test.options...); !errors.Is(err, test.code) {
			t.Fatal(name, err)
		}
	}
	// An unconfigured name becomes an INFO custom channel.
	owner, err = logging.PrepareChannels("default", map[logging.ChannelName]logging.ChannelSettings{"default": logging.DefaultChannelSettings()}, nil, logging.WithHandler("metrics", handler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Channels().Channel("metrics"); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncFileSinkDrainsEveryAcceptedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	sink, err := logging.PrepareSink(logging.SinkConfig{Driver: logging.File, Path: path, Async: logging.AsyncConfig{Enabled: true, Queue: 4096}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	logger := sink.Logger()
	for i := range 500 {
		logger.Info("record", "sequence", i)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stats := sink.Stats()
	if lines := strings.Count(string(data), "\n"); uint64(lines) != stats.Records || stats.Records+stats.Dropped != 500 || stats.Failures != 0 {
		t.Fatal("async sink lost accepted records", lines, stats)
	}
}

func TestGeneratedAsyncAndSyslogSettings(t *testing.T) {
	schema, err := logging.ChannelSettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"LOG__SINK__DRIVER": "syslog", "LOG__SINK__ASYNC__ENABLED": "true", "LOG__SINK__ASYNC__QUEUE": "64", "LOG__SINK__SYSLOG__FACILITY": "local2", "LOG__SINK__SYSLOG__TAG": "orders"}
	settings, _, err := schema.Load(logging.DefaultChannelSettings(), config.Inputs[logging.ChannelSettings]{
		Prefix:      "LOG",
		Environment: func(name string) (string, bool) { value, ok := values[name]; return value, ok },
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Sink.Driver != logging.Syslog || !settings.Sink.Async.Enabled || settings.Sink.Async.Queue != 64 || settings.Sink.Syslog.Facility != logging.FacilityLocal2 || settings.Sink.Syslog.Tag != "orders" {
		t.Fatal("generated async/syslog keys were not applied", settings.Sink)
	}
}
