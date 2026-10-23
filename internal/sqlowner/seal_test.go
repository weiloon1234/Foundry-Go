package sqlowner

import (
	"reflect"
	"testing"
)

func TestCapabilitiesRejectZeroAndAcceptIssuedCopies(t *testing.T) {
	issued := []interface{ Valid() bool }{Metadata(), Expressions(), Queries(), Observers(), Autocommit(), Outcomes(), Collections()}
	for _, capability := range issued {
		kind := reflect.TypeOf(capability)
		t.Run(kind.Name(), func(t *testing.T) {
			zero := reflect.Zero(kind).Interface().(interface{ Valid() bool })
			copied := reflect.ValueOf(capability).Interface().(interface{ Valid() bool })
			if zero.Valid() || !capability.Valid() || !copied.Valid() {
				t.Fatal("capability validity", zero.Valid(), capability.Valid(), copied.Valid())
			}
			for _, other := range issued {
				target := reflect.TypeOf(other)
				if kind != target && kind.ConvertibleTo(target) {
					t.Fatalf("capability families are convertible: %v to %v", kind, target)
				}
			}
		})
	}
	if (MetadataSeal{metadata: &observerCapability}).Valid() || (ObserverSeal{observer: new(sentinel)}).Valid() {
		t.Fatal("foreign family sentinel accepted")
	}
}
