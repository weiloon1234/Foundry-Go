package http_test

import (
	"context"
	"errors"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type floatCoordinate float32
type floatMeasurement float64

func TestURLFloatNativePrecision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want float64
	}{
		{"0", 0}, {"-0", math.Copysign(0, -1)}, {"+1.25", 1.25},
		{".5", .5}, {"1.", 1}, {"01.50", 1.5}, {"-2E-3", -.002},
		{"1e+20", 1e20}, {"5e-324", math.SmallestNonzeroFloat64},
		{"1.7976931348623157e308", math.MaxFloat64}, {"1e-1000", 0},
	} {
		got, err := foundryhttp.FloatQuery[floatMeasurement]().Parse(tc.text)
		if err != nil || math.Float64bits(float64(got)) != math.Float64bits(tc.want) {
			t.Fatalf("Parse(%q): %v, %v", tc.text, got, err)
		}
	}
	// Parsing at 64 bits then converting to 32 can double-round at a midpoint.
	// The native 32-bit parser must see the original decimal text.
	got, err := foundryhttp.FloatPath[floatCoordinate]().Parse("1.000000059604644775390626")
	if err != nil || math.Float32bits(float32(got)) != math.Float32bits(math.Nextafter32(1, 2)) {
		t.Fatalf("float32 midpoint rounding: %v, %v", got, err)
	}
	text, err := foundryhttp.FloatQuery[floatCoordinate]().Format(.1)
	if err != nil || text != "0.1" {
		t.Fatalf("float32 shortest output: %q, %v", text, err)
	}
	if got, err := foundryhttp.FloatPath[floatCoordinate]().Parse("3.4028235e38"); err != nil || float32(got) != math.MaxFloat32 {
		t.Fatalf("float32 maximum: %v, %v", got, err)
	}
	if got, err := foundryhttp.FloatQuery[floatCoordinate]().Parse("1e-45"); err != nil || float32(got) != math.SmallestNonzeroFloat32 {
		t.Fatalf("float32 subnormal: %v, %v", got, err)
	}
}

func TestURLFloatRejectsInvalidAndNonfinite(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", " ", "1 ", " 1", "1\n", ".", "+", "--1", "1e", "1e+", "1,2", "1_000", "0x1p2", "NaN", "Inf", "+Inf", "-Infinity", "∞", "١", "1e309", "-1e309", "private-url-float-value"} {
		got, err := foundryhttp.FloatQuery[floatMeasurement]().Parse(raw)
		if got != 0 || !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid float %q returned %v, %v", raw, got, err)
		}
		if strings.Contains(err.Error(), "private-url-float-value") {
			t.Fatalf("input appeared in failure: %q", raw)
		}
	}
	if _, err := foundryhttp.FloatPath[floatCoordinate]().Parse("3.5e38"); !errors.Is(err, fault.Invalid) {
		t.Fatalf("float32 overflow accepted: %v", err)
	}
	for _, number := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		text, err := foundryhttp.FloatQuery[floatMeasurement]().Format(floatMeasurement(number))
		if text != "" || !errors.Is(err, fault.Invalid) {
			t.Fatalf("nonfinite output: %q, %v", text, err)
		}
	}
}

func TestURLFloatPathAndQueryTransport(t *testing.T) {
	t.Parallel()
	type position struct{ Coordinate floatCoordinate }
	path := foundryhttp.DefinePath("/positions/{coordinate}", foundryhttp.Param("coordinate", foundryhttp.FloatPath[floatCoordinate](), func(p *position) *floatCoordinate { return &p.Coordinate }))
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "position.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, path)
	var seen floatCoordinate
	router, err := foundryhttp.NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p position) {
		seen = p.Coordinate
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	location, err := route.URL(position{Coordinate: 1e20})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
	if recorder.Code != 204 || seen != 1e20 {
		t.Fatal("path float did not round trip")
	}
	type filter struct {
		Minimum value.Optional[floatMeasurement]
		Points  []floatCoordinate
	}
	query := foundryhttp.DefineQuery(
		foundryhttp.OptionalQueryParam("minimum", foundryhttp.FloatQuery[floatMeasurement](), func(q *filter) *value.Optional[floatMeasurement] { return &q.Minimum }),
		foundryhttp.RepeatedQueryParam("point", foundryhttp.FloatQuery[floatCoordinate](), func(q *filter) *[]floatCoordinate { return &q.Points }),
	)
	limits := foundryhttp.QueryLimits{Bytes: 1024, Pairs: 8, Issues: 4}
	text, err := query.Encode(context.Background(), filter{Minimum: value.Set(floatMeasurement(1e20)), Points: []floatCoordinate{.1, -2}}, limits)
	if err != nil || text != "minimum=1e%2B20&point=0.1&point=-2" {
		t.Fatalf("query float escaping: %q, %v", text, err)
	}
	got, err := query.Decode(context.Background(), text, limits)
	minimum, present := got.Minimum.Get()
	if err != nil || !present || minimum != 1e20 || len(got.Points) != 2 || got.Points[0] != .1 || got.Points[1] != -2 {
		t.Fatalf("query float round trip: %+v, %v", got, err)
	}
	if _, err := query.Decode(context.Background(), "minimum=1e+20", limits); err == nil {
		t.Fatal("unescaped query plus was treated as an exponent sign")
	}
}

func FuzzURLFloatRoundTrip(f *testing.F) {
	for _, bits := range []uint64{0, 1, 0x8000000000000000, 0x3fb999999999999a, 0x7fefffffffffffff, 0x7ff0000000000000, 0x7ff8000000000000, 0xffffffff} {
		f.Add(bits)
	}
	f.Fuzz(func(t *testing.T, bits uint64) {
		number64 := math.Float64frombits(bits)
		codec64 := foundryhttp.FloatQuery[floatMeasurement]()
		text64, err64 := codec64.Format(floatMeasurement(number64))
		if math.IsNaN(number64) || math.IsInf(number64, 0) {
			if err64 == nil || text64 != "" {
				t.Fatal("nonfinite float64 encoded")
			}
		} else {
			got, err := codec64.Parse(text64)
			if err64 != nil || err != nil || math.Float64bits(float64(got)) != bits {
				t.Fatal("float64 bits did not round trip")
			}
		}
		number32 := math.Float32frombits(uint32(bits))
		codec32 := foundryhttp.FloatPath[floatCoordinate]()
		text32, err32 := codec32.Format(floatCoordinate(number32))
		if math.IsNaN(float64(number32)) || math.IsInf(float64(number32), 0) {
			if err32 == nil || text32 != "" {
				t.Fatal("nonfinite float32 encoded")
			}
		} else {
			got, err := codec32.Parse(text32)
			if err32 != nil || err != nil || math.Float32bits(float32(got)) != uint32(bits) {
				t.Fatal("float32 bits did not round trip")
			}
		}
	})
}
