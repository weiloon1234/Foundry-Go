package goway_test

import (
	"slices"
	"strings"
	"testing"
)

// legacyUpdateLocks requires the new lock at explicit zero-based statement
// positions, then adapts only those locks for comparison with frozen records.
func legacyUpdateLocks(t *testing.T, capture *goldenCapture, positions ...int) string {
	t.Helper()
	statements := strings.Split(strings.TrimSuffix(capture.sql.String(), "\n"), "\n")
	for _, position := range positions {
		if position < 0 || position >= len(statements) {
			t.Fatalf("missing update read at statement %d", position)
		}
	}
	remaining := capture.output.String()
	var normalized strings.Builder
	for position, sql := range statements {
		before, after, ok := strings.Cut(remaining, oracleSnapshot(sql))
		if !ok {
			t.Fatalf("statement %d missing from transcript", position)
		}
		if slices.Contains(positions, position) {
			if !strings.HasPrefix(sql, "SELECT ") || !strings.HasSuffix(sql, " FOR NO KEY UPDATE") {
				t.Fatalf("update read at statement %d lost FOR NO KEY UPDATE: %s", position, sql)
			}
			sql = strings.TrimSuffix(sql, " FOR NO KEY UPDATE") + " FOR UPDATE"
		}
		normalized.WriteString(before)
		normalized.WriteString(oracleSnapshot(sql))
		remaining = after
	}
	normalized.WriteString(remaining)
	return normalized.String()
}
