package bench

import "testing"

// BenchmarkInsertCompile has the same limitation as the baseline: writePlan.compile
// is private. A capture-driver terminal would include execution-path overhead.
func BenchmarkInsertCompile(b *testing.B) {
	b.Skip("no public insert-only compiler; execution-boundary measurements need separate approval")
}
