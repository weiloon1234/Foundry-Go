package application

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/observability"
	"log/slog"
)

type options struct {
	clock          clock.Clock
	logger         *slog.Logger
	recorder       *observability.Recorder
	reporters      []observability.Reporter
	exporters      []observability.TraceExporter
	batchExporters []observability.TraceBatchExporter
	infrastructure []infrastructure.Option
	logHandlers    []logging.ChannelOption
}
type Option func(*options) error

// WithLogHandler binds a typed slog.Handler to a named log channel. The channel
// may be configured with the custom driver (selecting its minimum level) or be
// absent, in which case it is added at the default INFO minimum. The handler is
// borrowed: the application never closes it. See logging.WithHandler.
func WithLogHandler(name logging.ChannelName, handler slog.Handler) Option {
	return func(o *options) error {
		if credential.IsNil(handler) {
			return fault.New(fault.Invalid, "application log handler is nil")
		}
		o.logHandlers = append(o.logHandlers, logging.WithHandler(name, handler))
		return nil
	}
}

func WithClock(c clock.Clock) Option {
	return func(o *options) error {
		if credential.IsNil(c) {
			return fault.New(fault.Invalid, "application clock is nil")
		}
		o.clock = c
		return nil
	}
}

// WithLogger replaces the selected default channel with a borrowed logger.
// Other explicitly configured channels retain their own lifecycle.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) error {
		if l == nil {
			return fault.New(fault.Invalid, "application logger is nil")
		}
		o.logger = l
		return nil
	}
}
func WithObservability(r *observability.Recorder) Option {
	return func(o *options) error {
		if r == nil {
			return fault.New(fault.Invalid, "application recorder is nil")
		}
		o.recorder = r
		return nil
	}
}
func WithCredentials(name credentials.Name, p credentials.Provider) Option {
	return func(o *options) error {
		o.infrastructure = append(o.infrastructure, infrastructure.WithCredentials(name, p))
		return nil
	}
}

// WithMailDriver borrows a custom driver under a distinct configured driver name.
func WithMailDriver(name infrastructure.MailDriver, driver email.Driver) Option {
	return func(o *options) error {
		o.infrastructure = append(o.infrastructure, infrastructure.WithMailDriver(name, driver))
		return nil
	}
}
