package cache_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func snapshotKey(t *testing.T, n cache.Namespace, name cache.Name, key string) cache.EntryKey {
	t.Helper()
	v, err := cache.NewEntryKey(n, name, key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestTaggedSnapshotIsCanonicalOwnedAndVersionSensitive(t *testing.T) {
	base := snapshotKey(t, namespace, "profiles", "member")
	a := snapshotKey(t, namespace, "teams", "a")
	b := snapshotKey(t, namespace, "teams", "b")
	va, err := cache.NewTagVersion()
	if err != nil {
		t.Fatal(err)
	}
	vb, err := cache.NewTagVersion()
	if err != nil {
		t.Fatal(err)
	}
	stamps := []cache.TagStamp{{Key: a, Version: va}, {Key: b, Version: vb}, {Key: a, Version: va}}
	first, err := cache.NewTaggedKey(base, stamps)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.NewTaggedKey(base, []cache.TagStamp{stamps[1], stamps[0]})
	if err != nil {
		t.Fatal(err)
	}
	if first.DataKey() != second.DataKey() || first.FillKey() != second.FillKey() || first.DataKey() == base {
		t.Fatal("tag identity is not canonical/distinct")
	}
	stamps[0] = cache.TagStamp{}
	returned := first.Stamps()
	returned[0] = cache.TagStamp{}
	if got := first.Stamps(); len(got) != 2 || got[0].Key.Validate() != nil {
		t.Fatal("snapshot aliases input/output slices")
	}
	changed, err := cache.NewTaggedKey(base, []cache.TagStamp{{Key: a, Version: vb}, {Key: b, Version: va}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.DataKey() != first.DataKey() || changed.FillKey() == first.FillKey() || changed.Fingerprint() == first.Fingerprint() {
		t.Fatal("versions changed stable key or retained fill identity")
	}
	bytes := va.Bytes()
	bytes[0] ^= 255
	if copy, err := cache.ParseTagVersion(va.Bytes()); err != nil || copy != va {
		t.Fatal("version bytes alias storage", copy, err)
	}
	if _, err := cache.NewTaggedKey(base, []cache.TagStamp{{Key: a, Version: va}, {Key: a, Version: vb}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("conflicting duplicate accepted", err)
	}
	other := snapshotKey(t, cache.Namespace{Application: "other", Environment: "test"}, "teams", "a")
	for _, input := range [][]cache.TagStamp{nil, {{Key: a}}, {{Key: other, Version: va}}, make([]cache.TagStamp, cache.MaxTags+1)} {
		if _, err := cache.NewTaggedKey(base, input); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := cache.ParseTagVersion(make([]byte, cache.TagVersionBytes)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := cache.ParseTagVersion([]byte("short")); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero cache.TaggedKey
	if !errors.Is(zero.Validate(), fault.Invalid) || zero.DataKey().Validate() == nil || zero.FillKey().Validate() == nil || len(zero.Stamps()) != 0 {
		t.Fatal("zero snapshot accepted")
	}
}
