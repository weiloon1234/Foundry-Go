package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestURLFloatGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "fmt"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Latitude float32
type Distance float64
type DistanceAlias = Distance
type Distances []DistanceAlias
type Fraction float64
func(v Fraction)MarshalText()([]byte,error){if v!=0.5{return nil,fmt.Errorf("invalid fraction")};return []byte("half"),nil}
func(v *Fraction)UnmarshalText(text []byte)error{if string(text)!="half"{return fmt.Errorf("invalid fraction")};*v=0.5;return nil}
//foundry:query
type SearchInput struct {
 Latitude Latitude
 Distances Distances
 Minimum value.Optional[DistanceAlias]
 Fraction Fraction
}
//foundry:path pattern=/measurements/{latitude}/{distance}/{fraction}
type MeasurementPath struct { Latitude Latitude; Distance DistanceAlias; Fraction Fraction }
var Parameters = SearchInputDescriptor()
var Location = MeasurementPathDescriptor()
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for file, snippets := range map[string][]string{
		"search_input_foundry.gen.go":     {"FloatQuery[Latitude]", "FloatQuery[DistanceAlias]", "RepeatedQueryParam[SearchInput, DistanceAlias, Distances]", "OptionalQueryParam[SearchInput, DistanceAlias]", "TextQuery[Fraction, *Fraction]"},
		"measurement_path_foundry.gen.go": {"FloatPath[Latitude]", "FloatPath[DistanceAlias]", "TextPath[Fraction, *Fraction]"},
	} {
		for _, want := range snippets {
			if !strings.Contains(first[file], want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("float generation changed between runs")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "float_runtime_test.go", `package sample
import (
 "math"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)
func TestFloatConsumer(t *testing.T) {
 limits:=foundryhttp.QueryLimits{Bytes:4096,Pairs:32,Issues:8}
 input,err:=Parameters.Decode(t.Context(),"latitude=1.000000059604644775390626&distances=0.1&distances=1e%2B20&minimum=-0&fraction=half",limits)
 if err!=nil{t.Fatal(err)}
 if float32(input.Latitude)!=math.Nextafter32(1,2) || len(input.Distances)!=2 || input.Distances[0]!=0.1 || input.Distances[1]!=1e20{t.Fatal("float precision changed")}
 minimum,present:=input.Minimum.Get();if !present || !math.Signbit(float64(minimum)){t.Fatal("signed optional zero lost")}
 if input.Fraction!=0.5{t.Fatal("custom codec lost precedence")}
 text,err:=Parameters.Encode(t.Context(),input,limits);if err!=nil || !strings.Contains(text,"fraction=half"){t.Fatalf("encode: %q, %v",text,err)}
 if _,err:=Parameters.Decode(t.Context(),"latitude=3.5e38&fraction=half",limits);err==nil{t.Fatal("float32 overflow accepted")}
 if _,err:=Parameters.Decode(t.Context(),"latitude=1&fraction=0.5",limits);err==nil{t.Fatal("custom text codec bypassed")}
 location,err:=Location.URL(MeasurementPath{Latitude:0.1,Distance:1e20,Fraction:0.5});if err!=nil{t.Fatal(err)}
 route:=foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID:"measurements.show",Method:foundryhttp.GET,Access:foundryhttp.Public},Location)
 var received MeasurementPath
 router,err:=foundryhttp.NewRouter(route.HandleRaw(func(w http.ResponseWriter,_ *http.Request,p MeasurementPath){received=p;w.WriteHeader(204)}));if err!=nil{t.Fatal(err)}
 response:=httptest.NewRecorder();router.ServeHTTP(response,httptest.NewRequest("GET",location,nil))
 if response.Code!=204 || received.Latitude!=0.1 || received.Distance!=1e20 || received.Fraction!=0.5{t.Fatal("generated float path changed values")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated float consumer failed: %v\n%s", err, output)
	}
}
