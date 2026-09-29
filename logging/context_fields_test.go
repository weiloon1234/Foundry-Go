package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func nowForTest() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }

type opaque struct{ Password string }

func TestContextAttrsAreScopedBoundedAndRedacted(t *testing.T) {
	ctx := logging.WithAttrs(t.Context(), slog.String("tenant", "t1"), slog.Int("attempt", 1), slog.String("password", "private-one"),
		slog.Any("model", opaque{"private-two"}), slog.Any("token", secret.New("private-three")), slog.Group("request", slog.String("route", "orders.show"), slog.Group("nested", slog.String("x", "y"))),
		slog.String("long", strings.Repeat("é", logging.MaxContextValueBytes)))
	child := logging.WithAttrs(ctx, slog.String("tenant", "t2"), slog.Any("err", errors.New("private-four")))
	var output bytes.Buffer
	logger := logging.JSON(&output, logging.Options{})
	logger.InfoContext(child, "child")
	logger.InfoContext(ctx, "parent")
	logger.InfoContext(t.Context(), "outside")
	if strings.Contains(output.String(), "private-") {
		t.Fatal("context fields leaked a credential or opaque value", output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var child0, parent map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &child0); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &parent); err != nil {
		t.Fatal(err)
	}
	fields := child0["context"].(map[string]any)
	if fields["tenant"] != "t2" || fields["attempt"] != float64(1) || fields["password"] != secret.Redacted || fields["token"] != secret.Redacted || fields["model"] != nil || fields["err"] != nil {
		t.Fatal("context fields were not merged and filtered", fields)
	}
	request := fields["request"].(map[string]any)
	if request["route"] != "orders.show" || request["nested"] != nil || len(fields["long"].(string)) > logging.MaxContextValueBytes {
		t.Fatal("group depth or value bound not applied", fields)
	}
	if parent["context"].(map[string]any)["tenant"] != "t1" || strings.Contains(lines[2], "context") {
		t.Fatal("context fields escaped their scope")
	}
	many := t.Context()
	for i := range 2 * logging.MaxContextFields {
		many = logging.WithAttrs(many, slog.Int(strings.Repeat("k", i%60+1)+string(rune('a'+i%26)), i))
	}
	if len(logging.AttrsFromContext(many)) > logging.MaxContextFields {
		t.Fatal("context field count is unbounded")
	}
}

func TestContextFieldsCrossAsynchronousBoundaries(t *testing.T) {
	ctx := logging.WithAttrs(t.Context(), slog.String("tenant", "t1"), slog.Int64("big", 1<<62), slog.Float64("ratio", 0.5), slog.Bool("beta", true),
		slog.Duration("budget", time.Second), slog.Group("request", slog.String("id", "r-1")))
	data, err := json.Marshal(logging.FieldsFromContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	var restored logging.ContextFields
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logging.JSON(&output, logging.Options{}).InfoContext(restored.Context(t.Context()), "job")
	for _, want := range []string{`"tenant":"t1"`, `"big":4611686018427387904`, `"ratio":0.5`, `"beta":true`, `"budget":"1s"`, `"request":{"id":"r-1"}`} {
		if !strings.Contains(output.String(), want) {
			t.Fatal("restored fields lost", want, output.String())
		}
	}
	for _, invalid := range []string{`[]`, `{"a":[1]}`, `{"a":null}`, `{"a":{"b":{"c":1}}}`, `{"a":1e999999}`} {
		if err := json.Unmarshal([]byte(invalid), &restored); err == nil {
			t.Fatal("invalid snapshot accepted", invalid)
		}
	}
	if err := json.Unmarshal([]byte(`{"password":"private-five","ok":"x"}`), &restored); err != nil {
		t.Fatal(err)
	}
	if attrs := restored.Attrs(); len(attrs) != 2 || attrs[1].Value.String() != secret.Redacted {
		t.Fatal("decoded snapshot bypassed redaction", attrs)
	}
}
