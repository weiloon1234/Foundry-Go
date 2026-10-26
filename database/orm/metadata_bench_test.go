package orm

import "testing"

// This measures native declaration validation. Historical generated metadata
// measurements remain historical; the distinct workloads are not equated.
func BenchmarkModelMetadataValidation(b *testing.B) {
	fields := timedRecords()
	if err := Check(fields); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := Check(fields); err != nil {
			b.Fatal(err)
		}
	}
}
