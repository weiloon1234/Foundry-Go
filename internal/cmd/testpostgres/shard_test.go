package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
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

func TestHeavyPackagesRunFirstAndKeepTheirShardAcrossModules(t *testing.T) {
	heavy := []string{"pkg/f", "other/root", "pkg/e", "pkg/b", "pkg/a", "pkg/f"}
	exempt := []string{"pkg/a"}
	for _, tc := range []struct {
		name     string
		packages []string
		shard    packageShard
		want     []string
	}{
		{"first", []string{"pkg/d", "pkg/b", "pkg/a", "pkg/f", "pkg/c", "pkg/e"}, packageShard{1, 2}, []string{"pkg/f", "pkg/e", "pkg/c"}},
		{"second", []string{"pkg/d", "pkg/b", "pkg/a", "pkg/f", "pkg/c", "pkg/e"}, packageShard{2, 2}, []string{"pkg/b", "pkg/d"}},
		{"other-first", []string{"other/root", "other/child"}, packageShard{1, 2}, []string{"other/child"}},
		{"other-second", []string{"other/root", "other/child"}, packageShard{2, 2}, []string{"other/root"}},
		{"single", []string{"pkg/d", "pkg/b", "pkg/a", "pkg/f", "pkg/c", "pkg/e"}, packageShard{1, 1}, []string{"pkg/b", "pkg/c", "pkg/d", "pkg/e", "pkg/f"}},
		{"empty", nil, packageShard{1, 2}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, originalHeavy := slices.Clone(tc.packages), slices.Clone(heavy)
			got := partitionPackages(tc.packages, tc.shard, exempt, heavy)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if !slices.Equal(tc.packages, original) || !slices.Equal(heavy, originalHeavy) {
				t.Fatal("partition mutated caller-owned packages or heavy list")
			}
		})
	}
}

func TestHeavyPackageShardsRemainCompleteDisjointAndStable(t *testing.T) {
	packages := []string{"pkg/g", "pkg/b", "pkg/d", "pkg/a", "pkg/f", "pkg/e", "pkg/c"}
	sorted := slices.Clone(packages)
	slices.Sort(sorted)
	heavy := []string{"pkg/f", "absent", "pkg/c", "pkg/f", "pkg/a"}
	exempt := []string{"pkg/a", "pkg/e"}
	for _, total := range []int{1, 2, 3, 9} {
		seen := make(map[string]bool)
		for index := 1; index <= total; index++ {
			shard := packageShard{index, total}
			got := partitionPackages(packages, shard, exempt, heavy)
			if !slices.Equal(got, partitionPackages(sorted, shard, exempt, heavy)) {
				t.Fatal("partition depends on discovery order", shard)
			}
			for _, pkg := range got {
				if seen[pkg] || !slices.Contains(packages, pkg) || slices.Contains(exempt, pkg) {
					t.Fatal("duplicate, unknown or exempt package selected", shard, pkg)
				}
				seen[pkg] = true
			}
		}
		if len(seen) != len(packages)-len(exempt) {
			t.Fatal("shards lost package coverage", total, seen)
		}
	}
}

func TestMakeHeavyPackagesExistInListedModules(t *testing.T) {
	testkit.TrackExternalInputs(t)
	root := "../../.."
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var heavy []string
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "RACE_HEAVY_PACKAGES := "); ok {
			if heavy != nil {
				t.Fatal("heavy packages must have one owner assignment")
			}
			heavy = strings.Fields(value)
		}
	}
	if len(heavy) == 0 {
		t.Fatal("missing literal RACE_HEAVY_PACKAGES assignment in Makefile")
	}
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	t.Setenv("GOWORK", "off")
	var packages []string
	for _, dir := range moduleDirectories {
		listed, err := listPackages(t.Context(), goBinary, filepath.Join(root, dir), os.Environ())
		if err != nil {
			t.Fatal(dir, err)
		}
		packages = append(packages, listed...)
	}
	for index, pkg := range heavy {
		if slices.Index(heavy, pkg) != index {
			t.Error("duplicate RACE_HEAVY_PACKAGES entry", pkg)
		}
		if !slices.Contains(packages, pkg) {
			t.Error("remove stale RACE_HEAVY_PACKAGES entry: absent from all listed modules", pkg)
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
			got := partitionPackages(packages, shard, nil, nil)
			if !slices.Equal(got, partitionPackages(sorted, shard, nil, nil)) {
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
	if got := partitionPackages(packages, packageShard{index: 1, total: 2}, exempt, nil); !slices.Equal(got, []string{"pkg/a/child", "pkg/c"}) {
		t.Fatal("first shard", got)
	}
	if got := partitionPackages(packages, packageShard{index: 2, total: 2}, exempt, nil); !slices.Equal(got, []string{"pkg/b"}) {
		t.Fatal("second shard", got)
	}
	if got := partitionPackages(nil, packageShard{index: 1, total: 1}, nil, nil); len(got) != 0 {
		t.Fatal("empty input selected packages", got)
	}
}
