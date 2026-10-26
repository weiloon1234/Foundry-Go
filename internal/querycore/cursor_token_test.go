package querycore

import (
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCursorTransportRoundTripsExactDriverValues(t *testing.T) {
	for _, original := range []driver.Value{nil, "", "private 值", []byte{0, 1, 255}, int64(math.MaxInt64), int64(math.MinInt64), 1.2345678901234567, true, false, time.Date(2026, 9, 11, 10, 11, 12, 123456000, time.UTC)} {
		encoded, err := EncodeCursorValue(original)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeCursorValue(encoded)
		if err != nil || !reflect.DeepEqual(original, decoded) {
			t.Fatalf("cursor value changed: %T", original)
		}
	}
	for _, token := range []string{"", "invalid", strings.Repeat("a", MaxCursorBytes+1), base64.RawURLEncoding.EncodeToString([]byte(`{"v":2}`)), base64.RawURLEncoding.EncodeToString([]byte(`{} {}`))} {
		if _, err := ParseCursor[struct{}](token); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed cursor accepted")
		}
	}
	for _, v := range []driver.Value{math.NaN(), math.Inf(1)} {
		if _, err := EncodeCursorValue(v); err == nil {
			t.Fatal("nonfinite cursor accepted")
		}
	}
}

func FuzzCursorTransport(f *testing.F) {
	token, err := MakeCursor(strings.Repeat("a", 64), []driver.Value{int64(42)})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(token)
	f.Add("")
	f.Add("invalid")
	f.Fuzz(func(t *testing.T, token string) {
		cursor, err := ParseCursor[struct{}](token)
		if err == nil {
			if cursor.Token() != token {
				t.Fatal("transport changed token")
			}
			if err := cursor.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	})
}
