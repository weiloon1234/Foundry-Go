package goway_test

import (
	"fmt"
	"strings"
	"testing"
)

// Both workloads use public terminals and the same driver responses. The transcript
// includes SQL, bindings, every persisted value and explicitly loaded relation slots.
func TestOldAndNewDataLayersAgree(t *testing.T) {
	old, current := &goldenCapture{}, &goldenCapture{}
	oldGolden{old}.run()
	newGolden{current}.run()
	for name, capture := range map[string]*goldenCapture{"old": old, "new": current} {
		if capture.statements != 56 {
			t.Fatalf("%s executed %d statements, want all 56", name, capture.statements)
		}
	}
	if !oracleEqual(t, "golden/1", old.output.String(), current.output.String()) {
		t.Fatalf("old/new SQL, bindings or hydrated values differ:\n%s", goldenDiff(old.output.String(), current.output.String()))
	}
}

func goldenDiff(old, current string) string {
	a, b := strings.Split(old, "\n"), strings.Split(current, "\n")
	var diff strings.Builder
	for i := 0; i < max(len(a), len(b)); i++ {
		if i < len(a) && i < len(b) && a[i] == b[i] {
			continue
		}
		fmt.Fprintf(&diff, "@@ line %d @@\n", i+1)
		if i < len(a) {
			fmt.Fprintf(&diff, "- old: %s\n", a[i])
		}
		if i < len(b) {
			fmt.Fprintf(&diff, "+ new: %s\n", b[i])
		}
	}
	return diff.String()
}
