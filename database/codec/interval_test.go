package codec_test

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestIntervalCodecRetainsComponentsAndNull(t *testing.T) {
	v, _ := temporal.NewInterval(-14, 3, -25*time.Hour-time.Microsecond)
	c := codec.Interval()
	encoded, err := c.Bind(v)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Decode(encoded); err != nil || got != v || c.ParameterType() != codec.TypeInterval {
		t.Fatal(got, err)
	}
	if got, err := c.Decode([]byte(encoded.(string))); err != nil || got != v {
		t.Fatal(got, err)
	}
	current := v
	for _, source := range []any{nil, 123, "infinity", "2147483648 mons", "9223372036854776 microseconds"} {
		if err := c.Scan(&current).Scan(source); err == nil || current != v {
			t.Fatal(source, current, err)
		}
	}
	nullable := codec.Nullable(c)
	if got, err := nullable.Decode(nil); err != nil || !got.IsNull() {
		t.Fatal(got, err)
	}
	if bound, err := nullable.Bind(value.Of(temporal.Interval{})); err != nil || bound == nil {
		t.Fatal("present zero became NULL", bound, err)
	}
}
