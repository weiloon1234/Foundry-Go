package codec_test

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"testing"
)

func TestCodecDeclarationValidationDoesNotInvokeCallbacks(t *testing.T) {
	c := codec.New[int](func(int) (driver.Value, error) { t.Error("validation encoded"); return nil, nil }, func(any) (int, error) { t.Error("validation decoded"); return 0, nil })
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []codec.Codec[int]{{}, codec.New[int](nil, func(any) (int, error) { return 0, nil }), codec.New[int](func(int) (driver.Value, error) { return int64(1), nil }, nil)} {
		if invalid.Validate() == nil {
			t.Fatal("undefined codec accepted")
		}
	}
}
