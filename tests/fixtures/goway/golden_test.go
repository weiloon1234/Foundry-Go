package goway_test

import (
	"fmt"
	"strings"
	"testing"
)

// Both workloads use public terminals and the same driver responses. The transcript
// includes SQL, bindings, every persisted value and explicitly loaded relation slots.
func TestOldAndNewDataLayersAgree(t *testing.T) {
	current := &goldenCapture{}
	newGolden{current}.run()
	if current.statements != 56 {
		t.Fatalf("executed %d statements, want all 56", current.statements)
	}
	if !oracleEqual(t, "golden/1", current.output.String()) {
		t.Fatal("frozen legacy SQL, bindings or hydration differs")
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
