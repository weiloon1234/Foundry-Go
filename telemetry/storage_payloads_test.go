package telemetry

import (
	"errors"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These literal representations pin persisted format 1 independently of the
// current Go struct layout. Add a format version before changing their meaning.
func TestStoragePayloadFormatOne(t *testing.T) {
	t.Run("histogram", func(t *testing.T) {
		input := store.Histogram{Count: 2, Seconds: .75, Buckets: []uint64{0, 0, 0, 0, 0, 1, 2, 2, 2, 2, 2, 2}}
		assertPayloadFormat(t, input, maxHistogramPayloadBytes, `{"version":1,"data":{"count":2,"seconds":0.75,"buckets":[0,0,0,0,0,1,2,2,2,2,2,2]}}`)
	})
	t.Run("runtime", func(t *testing.T) {
		input := Runtime{Health: Health{Ready: value.Of(true), State: foundation.Running, Mode: maintenance.Serving, Dependencies: []DependencyHealth{{ID: "database", State: health.Up}}}, Samples: []Sample{{Name: "jobs_total", Labels: `{role="worker"}`, Value: 12}}, Dropped: 3, StorageFailures: 4, Pending: 2}
		assertPayloadFormat(t, input, maxRuntimePayloadBytes, `{"version":1,"data":{"Health":{"Ready":true,"State":"running","Mode":"serving","Dependencies":[{"ID":"database","State":"up"}]},"Samples":[{"Name":"jobs_total","Labels":"{role=\"worker\"}","Value":12}],"Dropped":3,"StorageFailures":4,"Pending":2}}`)
	})
	t.Run("detail", func(t *testing.T) {
		input := Detail{Frames: []Frame{{File: "app.go", Function: "app.run", Line: 7, Column: 2}}, RequestID: "request-1", TraceID: tracing.TraceID{1}, Path: "/[redacted]", ClientIP: value.Set(netip.MustParseAddr("192.0.2.8")), Untrusted: true}
		assertPayloadFormat(t, input, maxDetailPayloadBytes, `{"version":1,"data":{"Frames":[{"File":"app.go","Function":"app.run","Line":7,"Column":2}],"RequestID":"request-1","TraceID":"01000000000000000000000000000000","Path":"/[redacted]","ClientIP":"192.0.2.8","Untrusted":true}}`)
	})
	t.Run("detail without optional address", func(t *testing.T) {
		input := Detail{Frames: []Frame{}, RequestID: "request-2"}
		assertPayloadFormat(t, input, maxDetailPayloadBytes, `{"version":1,"data":{"Frames":[],"RequestID":"request-2","TraceID":"00000000000000000000000000000000","Path":"","Untrusted":false}}`)
	})
	t.Run("receipt", func(t *testing.T) {
		input := receiptState{Rules: []ruleProgress{{Rule: RuleLatency, Bad: 2, Healthy: 1}}, Counters: []Sample{{Name: "jobs_total", Labels: "", Value: 12}}, Dropped: 3, StorageFailures: 4, StorageDropped: 5}
		assertPayloadFormat(t, input, maxReceiptPayloadBytes, `{"version":1,"data":{"rules":[{"rule":"latency","Bad":2,"Healthy":1}],"counters":[{"Name":"jobs_total","Labels":"","Value":12}],"Dropped":3,"StorageFailures":4,"storage_dropped":5}}`)
	})
}

func assertPayloadFormat[T any](t *testing.T, input T, limit int, fixture string) {
	t.Helper()
	encoded, err := encodePayload(input, limit)
	if err != nil {
		t.Fatal(err)
	}
	want, err := value.ParseJSON[store.Envelope[T]](fixture)
	if err != nil {
		t.Fatalf("format fixture is not valid typed JSON: %v", err)
	}
	gotText, err := encoded.Text()
	if err != nil {
		t.Fatal(err)
	}
	wantText, err := want.Text()
	if err != nil {
		t.Fatal(err)
	}
	if gotText != wantText {
		t.Fatal("persisted format 1 changed; version its payload before changing its representation")
	}
	for _, document := range []value.JSON[store.Envelope[T]]{encoded, want} {
		decoded, err := decodePayload(document, limit)
		if err != nil || !reflect.DeepEqual(decoded, input) {
			t.Fatalf("typed payload did not round trip: %v", err)
		}
	}
	if _, err := encodePayload(input, len(gotText)-1); !errors.Is(err, fault.Internal) {
		t.Fatal("encoding accepted a payload larger than its byte bound")
	}
	if _, err := decodePayload(encoded, len(gotText)-1); !errors.Is(err, fault.Internal) {
		t.Fatal("decoding accepted a payload larger than its byte bound")
	}
	for _, version := range []int{-1, 0, 2} {
		unknown, err := value.NewJSON(store.Envelope[T]{Version: version, Data: input})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodePayload(unknown, limit); !errors.Is(err, fault.Internal) {
			t.Fatalf("unsupported version %d was not a storage fault", version)
		}
	}
	if _, err := decodePayload(value.JSON[store.Envelope[T]]{}, limit); !errors.Is(err, fault.Internal) {
		t.Fatal("uninitialized JSON was accepted")
	}
}

func TestStoragePayloadsReturnOwnedValues(t *testing.T) {
	input := receiptState{Rules: []ruleProgress{{Rule: RuleLatency, Bad: 2}}, Counters: []Sample{{Name: "jobs_total", Value: 1}}}
	encoded, err := encodePayload(input, maxReceiptPayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	input.Rules[0].Bad = 9
	input.Counters[0].Value = 9
	first, err := decodePayload(encoded, maxReceiptPayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	first.Rules[0].Bad = 8
	first.Counters[0].Value = 8
	second, err := decodePayload(encoded, maxReceiptPayloadBytes)
	if err != nil || second.Rules[0].Bad != 2 || second.Counters[0].Value != 1 {
		t.Fatal("receipt payload retained caller or decoded slice storage")
	}
	cloned := second.clone()
	cloned.Rules[0].Bad = 7
	cloned.Counters[0].Value = 7
	if second.Rules[0].Bad != 2 || second.Counters[0].Value != 1 {
		t.Fatal("applied receipt progress was not an owned snapshot")
	}
}

func TestReceiptStateRejectsCorruptProgress(t *testing.T) {
	valid := receiptState{Rules: []ruleProgress{{Rule: RuleLatency, Bad: maxCaptures, Healthy: maxCaptures}}, Counters: []Sample{{Name: "jobs_total", Labels: strings.Repeat("x", maxSampleLabelBytes), Value: math.MaxFloat64}}, Dropped: math.MaxUint64, StorageFailures: math.MaxUint64, StorageDropped: math.MaxUint64}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	if err := (receiptState{}).validate(); err != nil {
		t.Fatal("empty initial progress is invalid", err)
	}
	for _, test := range []struct {
		name string
		edit func(*receiptState)
	}{
		{"unknown rule", func(s *receiptState) { s.Rules[0].Rule = "unregistered" }},
		{"duplicate rule", func(s *receiptState) { s.Rules = append(s.Rules, s.Rules[0]) }},
		{"too many rules", func(s *receiptState) { s.Rules = make([]ruleProgress, len(RuleValues())+1) }},
		{"negative bad count", func(s *receiptState) { s.Rules[0].Bad = -1 }},
		{"large bad count", func(s *receiptState) { s.Rules[0].Bad = maxCaptures + 1 }},
		{"negative healthy count", func(s *receiptState) { s.Rules[0].Healthy = -1 }},
		{"large healthy count", func(s *receiptState) { s.Rules[0].Healthy = maxCaptures + 1 }},
		{"too many counter baselines", func(s *receiptState) { s.Counters = make([]Sample, maxRuntimeSamples+1) }},
		{"large metric name", func(s *receiptState) { s.Counters[0].Name = strings.Repeat("x", observability.MaxMetricNameBytes+1) }},
		{"large label representation", func(s *receiptState) { s.Counters[0].Labels += "x" }},
		{"nan baseline", func(s *receiptState) { s.Counters[0].Value = math.NaN() }},
		{"infinite baseline", func(s *receiptState) { s.Counters[0].Value = math.Inf(1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := valid.clone()
			test.edit(&state)
			if err := state.validate(); !errors.Is(err, fault.Internal) {
				t.Fatal("corrupt progress was not rejected as a storage fault")
			}
		})
	}
}

func TestStoragePayloadRejectsOversizeDetail(t *testing.T) {
	input := Detail{Frames: []Frame{{File: strings.Repeat("private-payload-canary", maxDetailPayloadBytes)}}}
	if _, err := encodePayload(input, maxDetailPayloadBytes); err == nil || strings.Contains(err.Error(), "private-payload-canary") {
		t.Fatal("oversize detail was admitted or leaked by diagnostic formatting")
	}
}

func TestStoragePayloadBoundaryRejectsMalformedHistogram(t *testing.T) {
	for _, test := range []struct{ name, document string }{
		{"missing data", `{"version":1}`},
		{"null data", `{"version":1,"data":null}`},
		{"missing histogram field", `{"version":1,"data":{"count":1,"seconds":0.1}}`},
		{"negative count", `{"version":1,"data":{"count":-1,"seconds":0.1,"buckets":[]}}`},
		{"fractional bucket", `{"version":1,"data":{"count":1,"seconds":0.1,"buckets":[0.5]}}`},
		{"unknown field", `{"version":1,"data":{"count":1,"seconds":0.1,"buckets":[],"extra":true}}`},
		{"duplicate field", `{"version":1,"data":{"count":1,"count":2,"seconds":0.1,"buckets":[]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := value.ParseJSON[store.Envelope[store.Histogram]](test.document); !errors.Is(err, fault.Invalid) {
				t.Fatal("malformed histogram crossed the typed JSON storage boundary")
			}
		})
	}
}

func TestStoragePayloadBoundaryRejectsAbsentReceiptData(t *testing.T) {
	for _, document := range []string{`{"version":1}`, `{"version":1,"data":null}`} {
		if _, err := value.ParseJSON[store.Envelope[receiptState]](document); !errors.Is(err, fault.Invalid) {
			t.Fatal("absent receipt data crossed the typed JSON storage boundary")
		}
	}
}
