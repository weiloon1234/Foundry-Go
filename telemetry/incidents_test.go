package telemetry

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func TestIncidentProgressSaturatesWithoutLosingTransitions(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	s := &storage{collector: c, config: incidentConfig(RulePoolPressure)}
	for _, pool := range []float64{.95, 0} {
		for i := range maxCaptures * 2 {
			next, decisions, err := s.incidentProgress(batch{poolUsage: pool})
			if err != nil {
				t.Fatal(err)
			}
			s.progress.Rules = next
			if err := s.progress.validate(); err != nil {
				t.Fatal("long-running progress exceeded the receipt bound", err)
			}
			if i > 0 && len(decisions) != 1 {
				t.Fatal("saturating progress lost a reached transition")
			}
		}
		p := s.progress.Rules[0]
		if pool > 0 && (p.Bad != 2 || p.Healthy != 0) || pool == 0 && (p.Healthy != 2 || p.Bad != 0) {
			t.Fatalf("saturated progress = %+v", p)
		}
	}
}

func TestIncidentLossUsesBothCountersAndTreatsResetAsHealthy(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	config := incidentConfig(RuleTelemetryLoss)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s := &storage{collector: c, config: config}
	for _, step := range []struct {
		dropped, failures uint64
		bad               bool
	}{{1, 0, true}, {0, 0, false}, {0, 1, true}, {0, 1, false}} {
		b := batch{runtime: Runtime{Dropped: step.dropped, StorageFailures: step.failures}}
		next, decisions, err := s.incidentProgress(b)
		if err != nil || len(decisions) != 1 || decisions[0].bad != step.bad {
			t.Fatalf("loss decision = %+v, %v", decisions, err)
		}
		s.progress = receiptState{Rules: next, Dropped: step.dropped, StorageFailures: step.failures}
	}
}

func TestIncidentAuthenticationRuleCountsOnlyAuthenticationAndRateLimits(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	config := incidentConfig(RuleAuthenticationBurst)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s := &storage{collector: c, config: config}
	b := batch{issues: []observedIssue{{Source: SourceSecurity, Code: "permission_denied"}, {Source: SourceBackend, Code: "authentication_failed"}, {Source: SourceSecurity, Code: "environment_probe"}}}
	_, decisions, err := s.incidentProgress(b)
	if err != nil || len(decisions) != 1 || decisions[0].bad {
		t.Fatal("unrelated failures raised authentication burst", err)
	}
	b.issues = append(b.issues, observedIssue{Source: SourceSecurity, Code: "authentication_failed"}, observedIssue{Source: SourceSecurity, Code: "rate_limited"})
	_, decisions, err = s.incidentProgress(b)
	if err != nil || len(decisions) != 1 || !decisions[0].bad {
		t.Fatal("authentication plus rate limit did not raise burst", err)
	}
}

func TestIncidentHistogramOverflowCannotLookHealthy(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	s := &storage{collector: c, config: incidentConfig(RuleLatency)}
	buckets := make([]uint64, len(observability.DurationBuckets()))
	for i := range buckets {
		buckets[i] = math.MaxUint64
	}
	b := batch{buckets: []bucket{{Interval: time.Minute, Metric: http.RequestMetric{Count: math.MaxUint64, Buckets: buckets}}, {Interval: time.Minute, Metric: http.RequestMetric{Count: 1, Buckets: make([]uint64, len(buckets))}}}}
	if _, _, err := s.incidentProgress(b); !errors.Is(err, fault.Internal) {
		t.Fatalf("overflow was not a storage failure: %v", err)
	}
}

func TestIncidentEnabledPolicyRejectsExternallyStagedProgress(t *testing.T) {
	c, _ := newCollector(t, collectorConfig())
	s := &storage{collector: c, config: incidentConfig(RuleLatency)}
	if err := s.flush(t.Context(), []ruleProgress{}); !errors.Is(err, fault.Invalid) {
		t.Fatalf("externally staged progress = %v", err)
	}
}

func TestAcknowledgementRejectsUninitializedEngine(t *testing.T) {
	for _, e := range []*Engine{nil, {}} {
		if _, err := e.AcknowledgeIn(t.Context(), nil, AlertKey(digest("shape"))); !errors.Is(err, fault.Invalid) {
			t.Fatalf("uninitialized engine = %v", err)
		}
	}
}
