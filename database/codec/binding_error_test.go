package codec_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type unformattableBindingError struct{}

func (*unformattableBindingError) Error() string          { panic("private encoder error") }
func (*unformattableBindingError) Format(fmt.State, rune) { panic("private encoder formatter") }

func TestBindRedactsEncoderAndValidatorFailures(t *testing.T) {
	cause := &unformattableBindingError{}
	for _, c := range []codec.Codec[string]{
		codec.New(func(string) (driver.Value, error) { return nil, cause }, func(any) (string, error) { return "", nil }),
		codec.String[string]().Validated(func(string) error { return cause }),
	} {
		_, err := c.Bind("private")
		_, nullableErr := codec.Nullable(c).Bind(value.Of("private"))
		for _, err := range []error{err, nullableErr} {
			var retained *unformattableBindingError
			if !errors.Is(err, cause) || errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause {
				t.Fatal("binding cause lost")
			}
			if got := fmt.Sprintf("%v", err); got != "database value binding failed" {
				t.Fatal("unsafe binding diagnostic")
			}
			if got := fmt.Sprintf("%#v", err); got != "database value binding failed" {
				t.Fatal("unsafe Go diagnostic")
			}
		}
	}
}
