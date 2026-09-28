package http

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
)

func TestURLSourceTypePreservesCodecMetadataAndBehavior(t *testing.T) {
	native := IntegerQuery[scalarMetadataRank]()
	original := codecMetadata(t, native)
	wrapped := URLType("source.example/app.Rank", native)
	info := codecMetadata(t, wrapped)
	want := original
	want.Value.ID = "source.example/app.Rank"
	if !reflect.DeepEqual(info, want) {
		t.Fatal("source identity changed codec shape")
	}
	if codecMetadata(t, native).Value.ID != original.Value.ID {
		t.Fatal("source codec mutated")
	}
	for _, input := range []string{"0", "65535", "65536", "wrong"} {
		left, le := native.Parse(input)
		right, re := wrapped.Parse(input)
		if left != right || (le == nil) != (re == nil) {
			t.Fatal("codec semantics changed", input, le, re)
		}
	}
	if text, err := wrapped.Format(scalarMetadataRank(42)); err != nil || text != "42" {
		t.Fatal(text, err)
	}
	descriptor := enum.Describe("source.example/app", "State", enum.Case[scalarMetadataEnum]{Name: "Ready", Value: "ready"})
	enumeration := URLType("source.example/app.State", EnumQuery[scalarMetadataEnum, *scalarMetadataEnum](descriptor))
	first := codecMetadata(t, enumeration)
	first.Value.Cases[0][1] = 'X'
	if string(codecMetadata(t, enumeration).Value.Cases[0]) != `"ready"` {
		t.Fatal("source enum metadata shared")
	}
	if _, err := enumeration.Parse("unknown"); err == nil {
		t.Fatal("source identity bypassed enum membership")
	}
}

func TestURLSourceTypeRejectsMissingOrInvalidMetadata(t *testing.T) {
	field := func(v *string) *string { return v }
	for _, codec := range []PathCodec[string]{
		URLType[string]("source.String", nil),
		URLType("source.String", undescribedSourceCodec{}),
		URLType("", StringPath[string]()),
		URLType("bad\nidentity", StringPath[string]()),
	} {
		if _, err := DefinePath("/{v}", Param("v", codec, field)).Parameters(); err == nil {
			t.Fatal("invalid source path accepted")
		}
		if err := DefineQuery(QueryParam("v", codec, field)).Validate(); err == nil {
			t.Fatal("invalid source query accepted")
		}
		if _, err := codec.Parse("private"); err == nil {
			t.Fatal("invalid source codec parsed")
		}
	}
}

type undescribedSourceCodec struct{}

func (undescribedSourceCodec) Parse(v string) (string, error)  { return v, nil }
func (undescribedSourceCodec) Format(v string) (string, error) { return v, nil }
