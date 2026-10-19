package productionprofile_test

import (
	"testing"

	"foundry.test/consumer/productionprofile"
)

func TestProfileQueryShapes(t *testing.T) {
	if err := productionprofile.CheckJoinedFields(); err != nil {
		t.Fatal(err)
	}
	if err := productionprofile.Check(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkProfileQueryShapes(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if err := productionprofile.Check(); err != nil {
			b.Fatal(err)
		}
		if err := productionprofile.CheckJoinedFields(); err != nil {
			b.Fatal(err)
		}
	}
}
