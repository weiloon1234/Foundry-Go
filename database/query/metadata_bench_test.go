package query

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func BenchmarkModelMetadataValidation(b *testing.B) {
	definition := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { return v, nil }).definition
	if err := definition.Validate(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := definition.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
