package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestStructuredRedactionAndLoggerIsolation(t *testing.T) {
	var output bytes.Buffer
	global := slog.Default()
	logger := logging.JSON(&output, logging.Options{Level: slog.LevelWarn})
	logger.Info("suppressed")
	if output.Len() != 0 {
		t.Fatal("level ignored")
	}
	logger.Warn("safe message", "database.password", "private-one", "Authorization", "private-two", "custom", secret.New("private-three"), "request_id", "visible", slog.Group("credentials", "nested", slog.GroupValue(slog.String("raw", "private-four"))))
	if strings.Contains(output.String(), "private-") {
		t.Fatalf("log leaked credentials: %s", output.String())
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["request_id"] != "visible" || event["database.password"] != secret.Redacted {
		t.Fatal("structured values lost")
	}
	output.Reset()
	logger.WithGroup("credentials").With("raw", "private-five").Warn("safe")
	if strings.Contains(output.String(), "private-") {
		t.Fatal("WithGroup credentials leaked")
	}
	if slog.Default() != global {
		t.Fatal("global logger replaced")
	}
}

func TestJSONTimestampZoneIsSnapshottedAndDefaultsUTC(t *testing.T) {
	zone := time.FixedZone("+08:00", 8*60*60)
	var local, utc bytes.Buffer
	logger := logging.JSON(&local, logging.Options{Location: zone})
	*zone = *time.UTC
	record := slog.NewRecord(time.Date(2026, 9, 25, 17, 30, 0, 0, time.FixedZone("source", 2*60*60)), slog.LevelInfo, "entry", 0)
	record.Add("password", "hidden")
	if err := logger.Handler().Handle(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if err := logging.JSON(&utc, logging.Options{}).Handler().Handle(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(local.String(), `"time":"2026-09-25T23:30:00+08:00"`) || strings.Contains(local.String(), "hidden") {
		t.Fatal("configured timezone or redaction lost", local.String())
	}
	if !strings.Contains(utc.String(), `"time":"2026-09-25T15:30:00Z"`) {
		t.Fatal("default is not UTC", utc.String())
	}
}
