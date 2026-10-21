package contract

import "testing"

func BenchmarkJSONDTODecode(b *testing.B) {
	descriptor := patchDescriptor()
	data := []byte(`{"id":"550e8400-e29b-41d4-a716-446655440000","age":37,"title":"member"}`)
	ctx, limits := b.Context(), dtoLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		result, err := descriptor.Decode(ctx, data, limits)
		if err != nil {
			b.Fatal(err)
		}
		if result.Age != 37 {
			b.Fatal("DTO decoding did not publish the complete result")
		}
	}
}
