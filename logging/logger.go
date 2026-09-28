// Package logging constructs application-owned structured loggers with safe
// defaults. It never changes slog.Default or installs a global subscriber.
package logging

import (
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/secret"
)

// Options configures the standard JSON handler.
type Options struct {
	Level     slog.Leveler
	AddSource bool
	// Location selects log timestamps. Nil means UTC. The value is snapshotted.
	Location *time.Location
}

// JSON uses slog's structured JSON handler, redacting common credential keys
// and respecting typed slog.LogValuer values such as secret.String. Unknown
// nested application objects must still use explicit safe logging contracts.
func JSON(writer io.Writer, options Options) *slog.Logger {
	location := *time.UTC
	if options.Location != nil {
		location = *options.Location
	}
	return slog.New(Correlate(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: options.Level, AddSource: options.AddSource,
		ReplaceAttr: func(groups []string, attribute slog.Attr) slog.Attr {
			if len(groups) == 0 && attribute.Key == slog.TimeKey && attribute.Value.Kind() == slog.KindTime {
				return slog.Time(attribute.Key, attribute.Value.Time().In(&location))
			}
			if sensitiveKey(attribute.Key) {
				return slog.String(attribute.Key, secret.Redacted)
			}
			for _, group := range groups {
				if sensitiveKey(group) {
					return slog.String(attribute.Key, secret.Redacted)
				}
			}
			return attribute
		},
	})))
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if dot := strings.LastIndexByte(key, '.'); dot >= 0 {
		key = key[dot+1:]
	}
	switch key {
	case "password", "password_hash", "secret", "client_secret", "token", "access_token", "refresh_token", "authorization", "cookie", "set_cookie", "api_key", "access_key", "secret_key", "credentials", "tracestate", "trace_state":
		return true
	}
	return false
}
