package oracle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplayRequiresEverySelectedOccurrence(t *testing.T) {
	b := sample()
	b.Records = map[string]Record{}
	for _, name := range []string{"TestCursor", "TestCursor/first", "TestCursor/last", "TestOther"} {
		for _, occurrence := range []string{"0", "1"} {
			b.Records["p/"+name+"/site/path/"+occurrence] = Record{Package: "p", Kind: "golden", Site: "site/path"}
		}
	}
	for _, tc := range []struct {
		run, skip string
		want      int
	}{
		{"", "", 8}, {"^TestCursor$", "", 6}, {"^TestCursor$/first", "", 4},
		{"^TestCursor$/last", "", 4}, {"", "^TestCursor$/last", 6},
		{"^TestCursor$", "^TestCursor$", 0}, {"", "^TestOther$", 6},
	} {
		selection, err := SelectTests(tc.run, tc.skip)
		if err != nil {
			t.Fatal(err)
		}
		missing, err := Unconsumed(b, "p", nil, selection)
		if err != nil || len(missing) != tc.want {
			t.Fatalf("run=%q skip=%q: %v, %v", tc.run, tc.skip, missing, err)
		}
	}
	selection, _ := SelectTests("^TestCursor$/last", "")
	seen := map[string]bool{"p/TestCursor/site/path/0": true, "p/TestCursor/site/path/1": true, "p/TestCursor/last/site/path/0": true}
	missing, err := Unconsumed(b, "p", seen, selection)
	if err != nil || len(missing) != 1 || missing[0] != "p/TestCursor/last/site/path/1" {
		t.Fatal(missing, err)
	}
}

func TestDirectorySupportsCheckoutAndPackagedConsumers(t *testing.T) {
	for _, layout := range []struct{ oracle, consumer string }{
		{"tests/fixtures/goway", "tests/fixtures/consumer/httpmodels"},
		{"consumers/goway", "consumers/full/httpmodels"},
		{"consumers/goway", "consumers/ordinary/models"},
		{"standalone", "standalone"},
	} {
		root := t.TempDir()
		expected := filepath.Join(root, layout.oracle, "testdata/oracles")
		start := filepath.Join(root, layout.consumer)
		if err := os.MkdirAll(expected, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(start, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(expected, "state.json"), []byte(`{"version":1,"phase":"pending"}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := directoryAt(start)
		if err != nil || got != expected {
			t.Fatal(got, expected, err)
		}
	}
}

func TestReplayPackageOwnershipDoesNotCaptureChildPackages(t *testing.T) {
	b := sample()
	b.Records = map[string]Record{
		"goway/TestA/site/0":              {Package: "goway", Kind: "golden", Site: "site"},
		"goway/integrations/TestB/site/0": {Package: "goway/integrations", Kind: "golden", Site: "site"},
	}
	selection, _ := SelectTests("", "")
	missing, err := Unconsumed(b, "goway", nil, selection)
	if err != nil || len(missing) != 1 || missing[0] != "goway/TestA/site/0" {
		t.Fatal(missing, err)
	}
}
