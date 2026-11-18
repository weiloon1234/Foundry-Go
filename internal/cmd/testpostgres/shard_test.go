package main

import (
	"slices"
	"testing"
)

func TestParseShard(t *testing.T) {
	for _, value := range []string{"", "1", "1/", "/2", "0/2", "1/0", "3/2", "-1/2", "1/-2", "a/2", "1/2/3", "999999999999999999999/2"} {
		if _, err := parseShard(value); err == nil {
			t.Errorf("accepted invalid shard %q", value)
		}
	}
	for _, value := range []string{"1/1", "1/2", "2/2", "3/7"} {
		if _, err := parseShard(value); err != nil {
			t.Errorf("rejected shard %q: %v", value, err)
		}
	}
}

func TestPackageShardsAreCompleteDisjointStableAndBalanced(t *testing.T) {
	packages := []string{"pkg/g", "pkg/b", "pkg/d", "pkg/a", "pkg/f", "pkg/e", "pkg/c"}
	original := slices.Clone(packages)
	sorted := slices.Clone(packages)
	slices.Sort(sorted)
	for _, total := range []int{1, 2, 3, 9} {
		seen := make(map[string]bool)
		minimum, maximum := len(packages), 0
		for index := 1; index <= total; index++ {
			shard := packageShard{index: index, total: total}
			got := partitionPackages(packages, shard, nil)
			if !slices.Equal(got, partitionPackages(sorted, shard, nil)) {
				t.Fatalf("shard %d/%d depends on discovery order", index, total)
			}
			minimum, maximum = min(minimum, len(got)), max(maximum, len(got))
			for _, pkg := range got {
				if seen[pkg] || !slices.Contains(packages, pkg) {
					t.Fatalf("duplicate or unknown package %s", pkg)
				}
				seen[pkg] = true
			}
		}
		if len(seen) != len(packages) || maximum-minimum > 1 {
			t.Fatalf("%d shards: coverage=%d, sizes=%d..%d", total, len(seen), minimum, maximum)
		}
	}
	if !slices.Equal(packages, original) {
		t.Fatal("partition changed caller-owned discovery result")
	}
	// Exemptions are exact package matches, removed before balancing shards.
	packages = []string{"pkg/a/child", "pkg/c", "pkg/a", "pkg/b"}
	exempt := []string{"pkg/a"}
	if got := partitionPackages(packages, packageShard{index: 1, total: 2}, exempt); !slices.Equal(got, []string{"pkg/a/child", "pkg/c"}) {
		t.Fatal("first shard", got)
	}
	if got := partitionPackages(packages, packageShard{index: 2, total: 2}, exempt); !slices.Equal(got, []string{"pkg/b"}) {
		t.Fatal("second shard", got)
	}
	if got := partitionPackages(nil, packageShard{index: 1, total: 1}, nil); len(got) != 0 {
		t.Fatal("empty input selected packages", got)
	}
}
