package application

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"log/slog"
	"slices"
	"time"
)

type ObservabilitySettings struct {
	Enabled, Maintenance                                             bool
	MaxSeries, MaxRecent, MaxActive, ErrorQueue, ReporterConcurrency int
	ReporterTimeout                                                  time.Duration
	SampleTraces                                                     bool
	// TraceSampleRatio selects the sampled fraction of traces; zero means 1.
	TraceSampleRatio                             float64
	TraceQueue, TraceConcurrency, TraceBatchSize int
	TraceTimeout                                 time.Duration
	ErrorLog                                     ErrorLogSettings
}

// ErrorLogSettings enables the built-in structured-log error reporter on the
// default logger. Window suppresses duplicate fingerprints (zero logs every
// report); DontReport filters skip matching fault codes or HTTP statuses.
type ErrorLogSettings struct {
	Enabled            bool
	Window             time.Duration
	MaxFingerprints    int
	DontReportFaults   []fault.Code `config:",json"`
	DontReportStatuses []int        `config:",json"`
}

func DefaultErrorLogSettings() ErrorLogSettings {
	c := observability.DefaultLogReporterConfig()
	return ErrorLogSettings{Window: c.Window, MaxFingerprints: c.MaxFingerprints}
}

func DefaultObservabilitySettings() ObservabilitySettings {
	c := observability.DefaultConfig()
	return ObservabilitySettings{MaxSeries: c.MaxSeries, MaxRecent: c.MaxRecent, MaxActive: c.MaxActive, ErrorQueue: c.ErrorQueue, ReporterConcurrency: c.ReporterConcurrency, ReporterTimeout: c.ReporterTimeout, SampleTraces: c.SampleTraces, TraceSampleRatio: c.TraceSampleRatio, TraceQueue: c.TraceQueue, TraceConcurrency: c.TraceConcurrency, TraceBatchSize: c.TraceBatchSize, TraceTimeout: c.TraceTimeout, ErrorLog: DefaultErrorLogSettings()}
}
func (s ObservabilitySettings) runtime() observability.Config {
	return observability.Config{MaxSeries: s.MaxSeries, MaxRecent: s.MaxRecent, MaxActive: s.MaxActive, ErrorQueue: s.ErrorQueue, ReporterConcurrency: s.ReporterConcurrency, ReporterTimeout: s.ReporterTimeout, SampleTraces: s.SampleTraces, TraceSampleRatio: s.TraceSampleRatio, TraceQueue: s.TraceQueue, TraceConcurrency: s.TraceConcurrency, TraceBatchSize: s.TraceBatchSize, TraceTimeout: s.TraceTimeout}
}
func (s ErrorLogSettings) reporter(logger *slog.Logger) (observability.Reporter, error) {
	c := observability.DefaultLogReporterConfig()
	c.Window, c.MaxFingerprints = s.Window, s.MaxFingerprints
	c.DontReportFaults, c.DontReportStatuses = s.DontReportFaults, s.DontReportStatuses
	return observability.LogReporter(logger, c)
}

// prepareObservation shares the application's maintenance gate with a
// configured recorder. Maintenance itself no longer requires observability.
func prepareObservation(s ObservabilitySettings, options options, logger *slog.Logger, gate *maintenance.Gate) (*observability.Recorder, error) {
	if options.recorder != nil {
		if len(options.reporters) > 0 || len(options.exporters) > 0 || len(options.batchExporters) > 0 || s.ErrorLog.Enabled {
			return nil, fault.New(fault.Invalid, "supplied observability cannot also declare reporters/exporters")
		}
		if s.Maintenance {
			return nil, fault.New(fault.Invalid, "supplied recorder owns its maintenance policy")
		}
		return options.recorder, nil
	}
	if !s.Enabled {
		if len(options.reporters) > 0 || len(options.exporters) > 0 || len(options.batchExporters) > 0 || s.ErrorLog.Enabled {
			return nil, fault.New(fault.Invalid, "observation declarations require observability enabled")
		}
		return nil, nil
	}
	c := s.runtime()
	c.Maintenance = gate
	c.TraceExporters = options.exporters
	c.TraceBatchExporters = options.batchExporters
	reporters := options.reporters
	if s.ErrorLog.Enabled {
		reporter, err := s.ErrorLog.reporter(logger)
		if err != nil {
			return nil, err
		}
		reporters = append(slices.Clone(reporters), reporter)
	}
	return observability.New(c, reporters...)
}
func WithErrorReporter(reporter observability.Reporter) Option {
	return func(o *options) error {
		if credential.IsNil(reporter) {
			return fault.New(fault.Invalid, "nil error reporter")
		}
		o.reporters = append(o.reporters, reporter)
		return nil
	}
}
func WithTraceExporter(exporter observability.TraceExporter) Option {
	return func(o *options) error {
		if credential.IsNil(exporter) {
			return fault.New(fault.Invalid, "nil trace exporter")
		}
		o.exporters = append(o.exporters, exporter)
		return nil
	}
}

// WithTraceBatchExporter adds a batch exporter such as
// observability.NewOTLPExporter to the configured recorder.
func WithTraceBatchExporter(exporter observability.TraceBatchExporter) Option {
	return func(o *options) error {
		if credential.IsNil(exporter) {
			return fault.New(fault.Invalid, "nil trace batch exporter")
		}
		o.batchExporters = append(o.batchExporters, exporter)
		return nil
	}
}
