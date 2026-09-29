package command

import (
	"io"
	"testing"
)

func TestOrphanCommandIsExplicitAndReadOnly(t *testing.T) {
	if _, err := Parse([]string{"metadata", "orphans", "--owner", "members", "--page-size", "2", "--format", "json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"metadata", "orphans"}, {"metadata", "orphans", "--owner", "members", "--delete"}, {"metadata", "orphans", "--owner", "members", "--apply"}, {"metadata", "orphans", "--owner", "members", "--page-size", "0"}, {"metadata", "orphans", "--owner", "members", "--page-size", "1001"}, {"metadata", "orphans", "--owner", "bad name"}, {"metadata", "orphans", "--owner", "members", "--format", "yaml"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
}

func TestRescopeCommandRequiresExplicitApply(t *testing.T) {
	inspect, err := Parse([]string{"metadata", "rescope", "--owner", "members"}, io.Discard)
	if err != nil || !inspect.options.Rescope() {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"metadata", "rescope", "--owner", "members", "--apply", "--format", "json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"metadata", "rescope"}, {"metadata", "rescope", "--owner", "members", "--delete"}, {"metadata", "rescope", "--owner", "members", "extra"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
}
