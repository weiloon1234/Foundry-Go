package http

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type scalarMetadataMember struct{}
type scalarMetadataCode string
type scalarMetadataRank uint16

func codecMetadata[V any](t *testing.T, codec PathCodec[V]) URLScalarInfo {
	t.Helper()
	selectValue := func(value *V) *V { return value }
	path := DefinePath("/{v}", Param("v", codec, selectValue))
	paths, err := path.Parameters()
	if err != nil || len(paths) != 1 || paths[0].Scalar == nil {
		t.Fatalf("path metadata: %+v %v", paths, err)
	}
	query := DefineQuery(QueryParam("v", codec, selectValue))
	parameters, err := query.Parameters()
	if err != nil || len(parameters) != 1 || parameters[0].Scalar == nil {
		t.Fatalf("query metadata: %+v %v", parameters, err)
	}
	if !reflect.DeepEqual(paths[0].Scalar, parameters[0].Scalar) {
		t.Fatal("path and query scalar descriptions differ")
	}
	return *paths[0].Scalar
}

func TestURLScalarMetadataMatchesNativeCodecs(t *testing.T) {
	cases := []struct {
		name     string
		describe func() URLScalarInfo
		kind     contract.Kind
		syntax   URLSyntax
		bits     uint8
		signed   bool
		format   contract.Format
	}{
		{"named_string", func() URLScalarInfo { return codecMetadata(t, StringPath[scalarMetadataCode]()) }, contract.StringKind, TextURLSyntax, 0, false, ""},
		{"native_int", func() URLScalarInfo { return codecMetadata(t, IntegerPath[int]()) }, contract.IntegerKind, IntegerURLSyntax, uint8(strconv.IntSize), true, ""},
		{"named_uint16", func() URLScalarInfo { return codecMetadata(t, IntegerPath[scalarMetadataRank]()) }, contract.IntegerKind, IntegerURLSyntax, 16, false, ""},
		{"int8", func() URLScalarInfo { return codecMetadata(t, IntegerPath[int8]()) }, contract.IntegerKind, IntegerURLSyntax, 8, true, ""},
		{"float32", func() URLScalarInfo { return codecMetadata(t, FloatPath[float32]()) }, contract.NumberKind, FloatURLSyntax, 32, false, ""},
		{"bool", func() URLScalarInfo { return codecMetadata(t, BoolPath[bool]()) }, contract.BooleanKind, BooleanURLSyntax, 0, false, ""},
		{"model_id", func() URLScalarInfo { return codecMetadata(t, ModelIDPath[scalarMetadataMember]()) }, contract.StringKind, ModelIDURLSyntax, 0, false, contract.UUIDFormat},
		{"decimal", func() URLScalarInfo { return codecMetadata(t, TextPath[decimal.Decimal, *decimal.Decimal]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.DecimalFormat},
		{"date", func() URLScalarInfo { return codecMetadata(t, TextPath[temporal.Date, *temporal.Date]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.DateFormat},
		{"time", func() URLScalarInfo { return codecMetadata(t, TextPath[temporal.Time, *temporal.Time]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.TimeFormat},
		{"datetime", func() URLScalarInfo { return codecMetadata(t, TextPath[temporal.DateTime, *temporal.DateTime]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.DateTimeFormat},
		{"local_datetime", func() URLScalarInfo {
			return codecMetadata(t, TextPath[temporal.LocalDateTime, *temporal.LocalDateTime]())
		}, contract.StringKind, FormattedURLSyntax, 0, false, contract.LocalDateTimeFormat},
		{"interval", func() URLScalarInfo { return codecMetadata(t, TextPath[temporal.Interval, *temporal.Interval]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.IntervalFormat},
		{"native_time", func() URLScalarInfo { return codecMetadata(t, TextPath[time.Time, *time.Time]()) }, contract.StringKind, FormattedURLSyntax, 0, false, contract.DateTimeFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := tc.describe()
			if info.Value.Kind != tc.kind || info.Syntax != tc.syntax || info.Value.Bits != tc.bits || info.Value.Signed != tc.signed || info.Value.Format != tc.format || info.Value.Nullable || info.Value.ID == "" {
				t.Fatalf("codec metadata: %+v", info)
			}
		})
	}
}

type scalarMetadataEnum string

func (v scalarMetadataEnum) MarshalText() ([]byte, error) { return []byte(v), nil }
func (v *scalarMetadataEnum) UnmarshalText(data []byte) error {
	*v = scalarMetadataEnum(data)
	return nil
}

func TestURLScalarMetadataOwnsEnumAndParameterSnapshots(t *testing.T) {
	descriptor := enum.Describe("foundry.test/scalar", "ScalarMetadataEnum",
		enum.Case[scalarMetadataEnum]{Name: "First", Value: "one"},
		enum.Case[scalarMetadataEnum]{Name: "Second", Value: "two"})
	codec := EnumPath[scalarMetadataEnum, *scalarMetadataEnum](descriptor)
	if _, err := codec.Parse("unlisted"); err == nil {
		t.Fatal("enum metadata disagreed with accepted values")
	}
	if _, err := codec.Format("unlisted"); err == nil {
		t.Fatal("unlisted enum value formatted")
	}
	if value, err := codec.Parse("one"); err != nil || value != "one" {
		t.Fatal("declared enum value rejected", err)
	}

	type parameters struct{ Left, Right scalarMetadataEnum }
	path := DefinePath("/{right}/{left...}",
		Param("left", codec, func(p *parameters) *scalarMetadataEnum { return &p.Left }),
		Param("right", codec, func(p *parameters) *scalarMetadataEnum { return &p.Right }))
	first, err := path.Parameters()
	if err != nil || len(first) != 2 || first[0].Name != "right" || first[0].CatchAll || first[1].Name != "left" || !first[1].CatchAll {
		t.Fatalf("path pattern metadata: %+v %v", first, err)
	}
	if first[0].Scalar.Syntax != EnumURLSyntax || len(first[0].Scalar.Value.Cases) != 2 {
		t.Fatal("enum metadata missing")
	}
	first[0].Scalar.Value.Cases[0][1] = 'x'
	first[0].Name = "changed"
	second, err := path.Parameters()
	if err != nil || second[0].Name != "right" || string(second[0].Scalar.Value.Cases[0]) != "\"one\"" || string(second[1].Scalar.Value.Cases[0]) != "\"one\"" {
		t.Fatal("path metadata snapshots shared enum bytes", err)
	}
	query := DefineQuery(RepeatedQueryParam("values", codec, func(p *[]scalarMetadataEnum) *[]scalarMetadataEnum { return p }))
	values, err := query.Parameters()
	if err != nil || len(values) != 1 || !values[0].Repeated || values[0].Required || values[0].Scalar.Value.Kind != contract.StringKind {
		t.Fatalf("repeated element contract: %+v %v", values, err)
	}
	values[0].Scalar.Value.Cases[0][1] = 'x'
	again, _ := query.Parameters()
	if string(again[0].Scalar.Value.Cases[0]) != "\"one\"" {
		t.Fatal("query metadata was shared")
	}
}

type scalarMetadataCustomCodec struct{ calls *int }

func (c *scalarMetadataCustomCodec) Parse(text string) (string, error) {
	(*c.calls)++
	return text, nil
}
func (c *scalarMetadataCustomCodec) Format(value string) (string, error) {
	(*c.calls)++
	return value, nil
}

func TestCustomURLScalarMetadataIsExplicitAndTyped(t *testing.T) {
	calls := 0
	codec := &scalarMetadataCustomCodec{calls: &calls}
	field := func(value *string) *string { return value }
	opaque := DefinePath("/{v}", Param("v", codec, field))
	parameters, err := opaque.Parameters()
	if err != nil || parameters[0].Scalar != nil || calls != 0 {
		t.Fatal("custom codec metadata was inferred or invoked")
	}
	declared := DescribeURL(codec, contract.DefineScalar[string](contract.Type{ID: "app.Code", Kind: contract.StringKind}))
	info := codecMetadata(t, declared)
	if info.Syntax != CustomURLSyntax || info.Value.ID != "app.Code" || calls != 0 {
		t.Fatal("custom metadata changed execution")
	}
	if parsed, err := declared.Parse("one"); err != nil || parsed != "one" || calls != 1 {
		t.Fatal("custom parser was replaced")
	}
	for _, invalid := range []PathCodec[string]{
		DescribeURL((*scalarMetadataCustomCodec)(nil), contract.DefineScalar[string](contract.Type{ID: "v", Kind: contract.StringKind})),
		DescribeURL(codec, contract.Scalar[string]{}),
	} {
		if _, err := DefinePath("/{v}", Param("v", invalid, field)).Parameters(); err == nil {
			t.Fatal("invalid metadata codec bound to path")
		}
		if err := DefineQuery(QueryParam("v", invalid, field)).Validate(); err == nil {
			t.Fatal("invalid metadata codec bound to query")
		}
		if _, err := invalid.Parse("one"); err == nil {
			t.Fatal("invalid codec parsed input")
		}
	}
}
