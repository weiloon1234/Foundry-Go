package tooling

import (
	"context"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func observedBuilder(exportSpan observability.TraceExporter, reportFailure observability.Reporter) (*foundation.Builder, error) {
	// doc-example:begin observability-1
	config := observability.DefaultConfig()
	config.TraceExporters = []observability.TraceExporter{exportSpan}
	recorder, err := observability.New(config, reportFailure)
	if err != nil {
		return nil, err
	}
	builder := foundation.NewBuilder(foundation.WithObservability(recorder))
	// doc-example:end observability-1
	return builder, nil
}

func queueCollector(recorder *observability.Recorder, queue interface{ Len() int }) error {
	// doc-example:begin observability-2
	err := recorder.RegisterCollector("orders.queue", func(w *observability.MetricWriter) {
		w.Gauge("orders_queue_depth", "Queued orders.", float64(queue.Len()), observability.Label{Name: "queue", Value: "default"})
	})
	if err != nil {
		return err
	}
	// doc-example:end observability-2
	return err
}

// doc-example:begin otlp-builder
func tracedBuilder(settings application.Settings, token string) (*application.Builder, error) {
	exporter, err := observability.NewOTLPExporter(observability.OTLPConfig{
		Endpoint:    "https://collector.example:4318/v1/traces",
		ServiceName: "orders",
		Headers:     []observability.OTLPHeader{{Name: "Authorization", Value: secret.New(token)}},
	})
	if err != nil {
		return nil, err
	}
	config := observability.DefaultConfig()
	config.TraceBatchExporters = []observability.TraceBatchExporter{exporter}
	recorder, err := observability.New(config)
	if err != nil {
		return nil, err
	}
	return application.New(settings, application.WithObservability(recorder)), nil
}

// doc-example:end otlp-builder
// doc-example:begin external-error-reporter
type externalEvent struct {
	Message     string
	Fingerprint []string
	Tags        map[string]string
	Frames      map[string]string
}

type deliverEvent func(context.Context, externalEvent) error

func externalReporter(deliver deliverEvent) observability.Reporter {
	return func(ctx context.Context, report observability.ErrorReport) error {
		event := externalEvent{
			Message:     string(report.Entry.Operation.Kind) + " " + string(report.Entry.Operation.Name) + " " + string(report.Entry.Result.Outcome),
			Fingerprint: []string{string(report.Entry.Operation.Name), strings.Join(report.Diagnostic.Types, ",")},
			Tags:        map[string]string{"trace_id": report.Entry.TraceID.String(), "request_id": string(report.Entry.RequestID)},
			Frames:      make(map[string]string, len(report.Diagnostic.Frames)),
		}
		for _, frame := range report.Diagnostic.Frames {
			event.Frames[frame.Function] = frame.File + ":" + strconv.Itoa(frame.Line)
		}
		return deliver(ctx, event)
	}
}

// doc-example:end external-error-reporter
