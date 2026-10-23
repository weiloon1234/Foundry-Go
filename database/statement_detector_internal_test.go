package database

import (
	"fmt"
	"strings"
	"testing"
)

func TestDisabledStatementDetectorKeepsNilInstrumentation(t *testing.T) {
	if newInstrumentation(poolSettings{}) != nil {
		t.Fatal("disabled detector installed instrumentation")
	}
}

func TestStatementFingerprintPrivacyAndSites(t *testing.T) {
	d, _ := NewStatementDetector(DefaultStatementDetectorConfig())
	other, _ := NewStatementDetector(DefaultStatementDetectorConfig())
	for _, sql := range []string{"SELECT 'canary'", "WITH bad SET token=canary", "SELECT 'unterminated", strings.Repeat("canary", 5000)} {
		if d.fingerprint(sql) != d.fingerprint(sql) || d.fingerprint(sql) == other.fingerprint(sql) || strings.Contains(d.fingerprint(sql), "canary") {
			t.Fatal("unstable or unkeyed fingerprint")
		}
	}
	ctx, scope := d.Begin(t.Context())
	for i := 0; i < 20; i++ {
		d.observe(ctx, QueryEvent{Statement: "select canary"}, fmt.Sprintf("handler.go:%d", i))
	}
	r := scope.Report()
	if !r.CallSiteOverflow || len(r.Violations) != 1 || len(r.Violations[0].CallSites) != 4 || !r.Violations[0].CallSiteOverflow {
		t.Fatal(r)
	}
	r.Violations[0].CallSites[0] = "changed"
	if scope.Report().Violations[0].CallSites[0] == "changed" {
		t.Fatal("aliased sites")
	}
}
