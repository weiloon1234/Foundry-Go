// The compatibility oracle intentionally imports only the standard library,
// so it can run with GOEXPERIMENT=nojsonv2.
package main

import (
	"encoding/json"
	"os"
)

type stringTextKey string

func (stringTextKey) MarshalText() ([]byte, error) { panic("string keys must ignore MarshalText") }

type stringAppendKey string

func (stringAppendKey) AppendText([]byte) ([]byte, error) {
	panic("string keys must ignore AppendText")
}

type pointerStringKey string

func (*pointerStringKey) MarshalText() ([]byte, error) { return []byte("mapped"), nil }

type integerTextKey int

func (integerTextKey) MarshalText() ([]byte, error) { return []byte("integer-method"), nil }

type structTextKey struct{ Text string }

func (k structTextKey) MarshalText() ([]byte, error) { return []byte(k.Text), nil }

func main() {
	pointer := pointerStringKey("original")
	cases := map[string]any{
		"string":                        map[string]int{"original": 1},
		"named string with text method": map[stringTextKey]int{"original": 1},
		"named string with appender":    map[stringAppendKey]int{"original": 1},
		"pointer":                       map[*pointerStringKey]int{&pointer: 1},
		"nil pointer":                   map[*pointerStringKey]int{nil: 1},
		"integer":                       map[int]int{-42: 1},
		"unsigned integer":              map[uint64]int{42: 1},
		"integer with text method":      map[integerTextKey]int{42: 1},
		"text marshaler struct":         map[structTextKey]int{{Text: "original"}: 1},
	}
	output := make(map[string]string, len(cases))
	for name, input := range cases {
		encoded, err := json.Marshal(input)
		if err != nil {
			panic(err)
		}
		output[name] = string(encoded)
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		panic(err)
	}
}
