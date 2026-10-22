//go:build !race

package ormcore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

// Performance parity is tested without safety instrumentation. checkptr=2
// deliberately moves pointer-conversion temporaries to the heap; the race +
// checkptr run exercises all value/scan/error cases in primitive_codec_test.go.
var boundSink driver.Value

func bindAllocations[V any](t *testing.T, got, want codec.Codec[V], v V) {
	t.Helper()
	baseline := testing.AllocsPerRun(100, func() {
		var err error
		boundSink, err = want.Bind(v)
		if err != nil {
			t.Fatal(err)
		}
	})
	actual := testing.AllocsPerRun(100, func() {
		var err error
		boundSink, err = got.Bind(v)
		if err != nil {
			t.Fatal(err)
		}
	})
	if actual > baseline {
		t.Fatalf("binding allocations: got %g, baseline %g", actual, baseline)
	}
}
func TestPrimitiveBindingAllocationParity(t *testing.T) {
	bindAllocations(t, NumberCodec[namedInt](), codec.Signed[namedInt](), namedInt(4096))
	bindAllocations(t, NumberCodec[namedInt8](), codec.Signed[namedInt8](), namedInt8(-4))
	bindAllocations(t, NumberCodec[namedInt16](), codec.Signed[namedInt16](), namedInt16(4096))
	bindAllocations(t, NumberCodec[namedInt32](), codec.Signed[namedInt32](), namedInt32(4096))
	bindAllocations(t, NumberCodec[namedInt64](), codec.Signed[namedInt64](), namedInt64(4096))
	bindAllocations(t, NumberCodec[namedUint](), codec.Unsigned[namedUint](), namedUint(4096))
	bindAllocations(t, NumberCodec[namedUint8](), codec.Unsigned[namedUint8](), namedUint8(4))
	bindAllocations(t, NumberCodec[namedUint16](), codec.Unsigned[namedUint16](), namedUint16(4096))
	bindAllocations(t, NumberCodec[namedUint32](), codec.Unsigned[namedUint32](), namedUint32(4096))
	bindAllocations(t, NumberCodec[namedUint64](), codec.Unsigned[namedUint64](), namedUint64(4096))
	bindAllocations(t, NumberCodec[namedFloat32](), codec.Float[namedFloat32](), namedFloat32(1.25))
	bindAllocations(t, NumberCodec[namedFloat64](), codec.Float[namedFloat64](), namedFloat64(1.25))
	bindAllocations(t, KeyScalarCodec[namedString](), codec.String[namedString](), namedString("text"))
	bindAllocations(t, ScalarCodec[namedBool](), codec.Bool[namedBool](), namedBool(true))
	bindAllocations(t, NumberCodec[decimal.Decimal](), codec.Decimal(), decimal.FromInt64(123))
}
