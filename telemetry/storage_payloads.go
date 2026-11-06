package telemetry

import (
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Incident evaluation belongs to the engine milestone. Storage accepts its
// concrete next progress, never an arbitrary policy callback inside a transaction.
type ruleProgress struct {
	Rule         Rule `json:"rule"`
	Bad, Healthy int
}

type receiptState struct {
	Rules                    []ruleProgress `json:"rules"`
	Counters                 []Sample       `json:"counters"`
	Dropped, StorageFailures uint64
	StorageDropped           uint64 `json:"storage_dropped"`
}

func (s receiptState) clone() receiptState {
	s.Rules = slices.Clone(s.Rules)
	s.Counters = slices.Clone(s.Counters)
	return s
}

func (s receiptState) validate() error {
	if len(s.Rules) > len(RuleValues()) || len(s.Counters) > maxRuntimeSamples {
		return corruptPayload()
	}
	seen := make(map[Rule]bool, len(s.Rules))
	for _, p := range s.Rules {
		if !p.Rule.IsValid() || seen[p.Rule] || p.Bad < 0 || p.Healthy < 0 || p.Bad > maxCaptures || p.Healthy > maxCaptures {
			return corruptPayload()
		}
		seen[p.Rule] = true
	}
	for _, sample := range s.Counters {
		if len(sample.Name) > observability.MaxMetricNameBytes || len(sample.Labels) > maxSampleLabelBytes || math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
			return corruptPayload()
		}
	}
	return nil
}

func encodePayload[T any](data T, limit int) (value.JSON[store.Envelope[T]], error) {
	encoded, err := value.NewJSON(store.Envelope[T]{Version: 1, Data: data})
	if err != nil {
		return encoded, safeerror.WrapFault(fault.Internal, "cannot encode telemetry payload", err)
	}
	text, err := encoded.Text()
	if err != nil || len(text) > limit {
		return value.JSON[store.Envelope[T]]{}, corruptPayload()
	}
	return encoded, nil
}

func decodePayload[T any](encoded value.JSON[store.Envelope[T]], limit int) (T, error) {
	var zero T
	text, err := encoded.Text()
	if err != nil || len(text) > limit {
		return zero, corruptPayload()
	}
	// value.JSON already rejects missing/null fields and owns shape validation.
	envelope, err := encoded.Decode()
	if err != nil || envelope.Version != 1 {
		return zero, corruptPayload()
	}
	return envelope.Data, nil
}

func corruptPayload() error {
	return safeerror.NewFault(fault.Internal, "invalid telemetry storage payload")
}
