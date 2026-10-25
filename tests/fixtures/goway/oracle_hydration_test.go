package goway_test

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"
)

func (g *goldenCapture) showHydrated(v any) {
	fmt.Fprintln(&g.output, oracleSnapshot([]any{oracle.ModelSnapshot(v)}))
}
func oracleSnapshot(v any) string { return oracle.Snapshot(v) }
func TestOracleSnapshotRetainsNilAndIgnoresPointerAddresses(t *testing.T) {
	type sample struct {
		Values []int
		Value  *int
	}
	a, b := 4, 4
	if oracleSnapshot(sample{Value: &a}) != oracleSnapshot(sample{Value: &b}) {
		t.Fatal("pointer address entered evidence")
	}
	if oracleSnapshot(sample{}) == oracleSnapshot(sample{Values: []int{}}) {
		t.Fatal("nil and empty collapsed")
	}
	if oracleSnapshot(sample{}) == oracleSnapshot(sample{Value: &a}) {
		t.Fatal("nil and present collapsed")
	}
}
