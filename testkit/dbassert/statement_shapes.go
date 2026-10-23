package dbassert

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// AssertStatementShapes asserts a completed test scope stayed within its configured
// per-shape threshold. Shape or call-site capacity overflow fails rather than silently
// passing incomplete observations. Close all query rows before asserting.
// Example: ctx,scope := detector.Begin(t.Context()); run(ctx); dbassert.AssertStatementShapes(t,scope).
func AssertStatementShapes(t testing.TB, scope *database.StatementScope) {
	t.Helper()
	assertStatementShapes(t, scope)
}
func assertStatementShapes(t interface {
	Helper()
	Errorf(string, ...any)
}, scope *database.StatementScope) {
	t.Helper()
	if scope == nil {
		t.Errorf("missing database statement scope")
		return
	}
	r := scope.Report()
	for _, v := range r.Violations {
		t.Errorf("database shape %s ran %d times (threshold %d), call sites %v", v.Shape, v.Count, v.Threshold, v.CallSites)
	}
	if r.Overflow {
		t.Errorf("database statement shape capacity exceeded; observation incomplete")
	}
	if r.CallSiteOverflow {
		t.Errorf("database statement call-site capacity exceeded; observation incomplete")
	}
}
