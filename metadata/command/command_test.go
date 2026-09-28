package command

import (
	"io"
	"testing"
)

func TestOrphanCommandIsExplicitAndReadOnly(t *testing.T) {
	if _, err := Parse([]string{"metadata", "orphans", "--owner", "members", "--page-size", "2", "--format", "json"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"metadata", "orphans"}, {"metadata", "orphans", "--owner", "members", "--delete"}, {"metadata", "orphans", "--owner", "members", "--page-size", "0"}, {"metadata", "orphans", "--owner", "members", "--page-size", "1001"}, {"metadata", "orphans", "--owner", "bad name"}, {"metadata", "orphans", "--owner", "members", "--format", "yaml"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
}
