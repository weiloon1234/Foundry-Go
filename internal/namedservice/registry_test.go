package namedservice_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"sync"
	"testing"
)

type name string

func (n name) Validate() error { return namedservice.Validate(string(n)) }
func TestNamedDefaultIdentityAndFrozenSelection(t *testing.T) {
	one, two := 1, 2
	entries := []namedservice.Entry[name, int]{{Name: "one", Value: &one}, {Name: "two", Value: &two}}
	r, err := namedservice.New(name("two"), entries)
	if err != nil {
		t.Fatal(err)
	}
	entries[1].Value = &one
	selected, err := r.Default()
	explicit, e := r.Get("two")
	if err != nil || e != nil || selected != &two || selected != explicit {
		t.Fatal("default was copied or selection changed")
	}
	names := r.Names()
	names[0] = "changed"
	if r.Names()[0] != "one" {
		t.Fatal("mutable registry names")
	}
	if _, err := r.Get("missing"); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if got, _ := r.Default(); got != &two {
				t.Error("identity changed")
			}
		})
	}
	wg.Wait()
	for _, tc := range []struct {
		selected name
		entries  []namedservice.Entry[name, int]
		kind     error
	}{{"missing", entries, fault.Missing}, {"one", append(entries, entries[0]), fault.Duplicate}, {"one", []namedservice.Entry[name, int]{{Name: "one"}}, fault.Invalid}, {"bad name", entries, fault.Invalid}} {
		if _, err := namedservice.New(tc.selected, tc.entries); !errors.Is(err, tc.kind) {
			t.Fatal(err)
		}
	}
}
