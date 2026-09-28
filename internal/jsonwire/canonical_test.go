package jsonwire

import (
	"bytes"
	"testing"
)

func TestCanonicalTransportPreservesMeaning(t *testing.T) {
	limits := Limits{Bytes: MaxBytes, Depth: MaxDepth, Nodes: MaxNodes}
	a, err := Canonical([]byte(` {"b":1.00e1,"a":"\u0000","list":[null,0,false]} `), limits)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical([]byte(`{"list":[null,-0.0,false],"a":"\u0000","b":10}`), limits)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("equivalent wire values diverged", err)
	}
	for _, different := range []string{`{}`, `{"a":null}`, `{"a":0}`, `{"a":""}`, `{"a":[]}`} {
		encoded, err := Canonical([]byte(different), limits)
		if err != nil {
			t.Fatal(err)
		}
		for _, other := range []string{`{}`, `{"a":null}`, `{"a":0}`, `{"a":""}`, `{"a":[]}`} {
			next, _ := Canonical([]byte(other), limits)
			if different != other && bytes.Equal(encoded, next) {
				t.Fatal("omitted/null/zero collapsed")
			}
		}
	}
	for _, bad := range []string{`{"a":1,"a":2}`, `"\ud800"`, `1e9999999`, `[1,]`} {
		if _, err := Canonical([]byte(bad), limits); err == nil {
			t.Fatal("invalid canonical input accepted")
		}
	}
	if _, _, err := Parse([]byte(`"\u0000"`)); err == nil {
		t.Fatal("storage NUL policy changed")
	}
	if _, err := Canonical([]byte(`1e100`), Limits{Bytes: 8, Depth: 1, Nodes: 3}); err == nil {
		t.Fatal("number expansion escaped byte limit")
	}
}
func FuzzCanonicalJSON(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":null,"b":1.0}`, `"\u0000"`, `[0,false]`, `1e-1000`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		limits := Limits{Bytes: 4096, Depth: 12, Nodes: 256}
		first, err := Canonical(data, limits)
		if err != nil {
			return
		}
		second, err := Canonical(first, limits)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatal("canonical JSON was not stable")
		}
	})
}
