package codec_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDecodeAndScanRedactWithoutReclassifying(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		private := &unformattableBindingError{}
		var cause error = private
		if invalid {
			cause = fault.Wrap(fault.Invalid, "invalid decoded value", private)
		}
		for _, c := range []codec.Codec[string]{
			codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "partial", cause }),
			codec.String[string]().Validated(func(string) error { return cause }),
		} {
			decoded, decodeErr := c.Decode("private")
			destination := "original"
			scanErr := c.Scan(&destination).Scan("private")
			if decoded != "" || destination != "original" {
				t.Fatal("failed decoding published a value")
			}
			for _, err := range []error{decodeErr, scanErr} {
				var retained *unformattableBindingError
				if !errors.Is(err, private) || !errors.As(err, &retained) || retained != private || errors.Is(err, fault.Invalid) != invalid {
					t.Fatal("decode classification or cause changed")
				}
				if got := fmt.Sprintf("%v", err); got != "database value decoding failed" && got != "database value scanning failed" {
					t.Fatal("unsafe decoding diagnostic")
				}
			}
		}
	}
}
