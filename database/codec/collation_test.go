package codec_test

import (
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

func TestCollationCapabilitySurvivesCodecAdapters(t *testing.T) {
	for _, base := range []codec.Codec[string]{codec.String[string](), codec.String[string]().WithoutCollation()} {
		want := base.Collatable()
		if base.ParameterType() != codec.TypeText {
			t.Fatal("collation metadata changed parameter representation")
		}
		if codec.Nullable(base).Collatable() != want || base.Validated(func(string) error { return nil }).Collatable() != want || codec.Adapt(base, func(v string) string { return v }, func(v string) string { return v }).Collatable() != want {
			t.Fatal("codec adapter lost collation capability")
		}
	}
	custom := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(v any) (string, error) { return v.(string), nil }).WithParameterType(codec.TypeText)
	if !custom.Collatable() || !codec.Signed[int64]().WithParameterType(codec.TypeText).Collatable() || codec.String[string]().WithParameterType(codec.TypeInteger).Collatable() {
		t.Fatal("text codecs must be collatable by default")
	}
}

func TestCustomTextCodecWithoutCollationSurvivesAdapters(t *testing.T) {
	base := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(v any) (string, error) { return v.(string), nil }).WithoutCollation()
	for _, c := range []codec.Codec[string]{base, base.Validated(func(string) error { return nil }), codec.Adapt(base, func(v string) string { return v }, func(v string) string { return v })} {
		if c.WithParameterType(codec.TypeText).Collatable() || codec.Nullable(c).WithParameterType(codec.TypeText).Collatable() {
			t.Fatal("codec adapter lost the non-collatable mark before its parameter type was set")
		}
	}
}
