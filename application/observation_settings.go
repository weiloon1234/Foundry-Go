package application

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/observability"
	"time"
)

type ObservabilitySettings struct {
	Enabled, Maintenance                                             bool
	MaxSeries, MaxRecent, MaxActive, ErrorQueue, ReporterConcurrency int
	ReporterTimeout                                                  time.Duration
	SampleTraces                                                     bool
	TraceQueue, TraceConcurrency                                     int
	TraceTimeout                                                     time.Duration
}

func DefaultObservabilitySettings() ObservabilitySettings {
	c := observability.DefaultConfig()
	return ObservabilitySettings{MaxSeries: c.MaxSeries, MaxRecent: c.MaxRecent, MaxActive: c.MaxActive, ErrorQueue: c.ErrorQueue, ReporterConcurrency: c.ReporterConcurrency, ReporterTimeout: c.ReporterTimeout, SampleTraces: c.SampleTraces, TraceQueue: c.TraceQueue, TraceConcurrency: c.TraceConcurrency, TraceTimeout: c.TraceTimeout}
}
func (s ObservabilitySettings) runtime() observability.Config {
	return observability.Config{MaxSeries: s.MaxSeries, MaxRecent: s.MaxRecent, MaxActive: s.MaxActive, ErrorQueue: s.ErrorQueue, ReporterConcurrency: s.ReporterConcurrency, ReporterTimeout: s.ReporterTimeout, SampleTraces: s.SampleTraces, TraceQueue: s.TraceQueue, TraceConcurrency: s.TraceConcurrency, TraceTimeout: s.TraceTimeout}
}
func prepareObservation(s ObservabilitySettings, options options) (*observability.Recorder, error) {
	if options.recorder != nil {
		if len(options.reporters) > 0 || len(options.exporters) > 0 {
			return nil, fault.New(fault.Invalid, "supplied observability cannot also declare reporters/exporters")
		}
		if s.Maintenance {
			return nil, fault.New(fault.Invalid, "supplied recorder owns its maintenance policy")
		}
		return options.recorder, nil
	}
	if !s.Enabled {
		if s.Maintenance || len(options.reporters) > 0 || len(options.exporters) > 0 {
			return nil, fault.New(fault.Invalid, "observation declarations require observability enabled")
		}
		return nil, nil
	}
	c := s.runtime()
	c.TraceExporters = options.exporters
	recorder, err := observability.New(c, options.reporters...)
	if err != nil {
		return nil, err
	}
	if s.Maintenance {
		if err := recorder.Gate().Set(true); err != nil {
			return nil, err
		}
	}
	return recorder, nil
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
