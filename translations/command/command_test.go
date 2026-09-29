package command

import (
	"io"
	"testing"
)

func TestTranslationsOrphanCommandRejectsMutationFlags(t *testing.T) {
	if _, err := Parse([]string{"translations", "orphans", "--owner", "products"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"translations", "orphans"}, {"translations", "orphans", "--owner", "products", "--delete"}, {"translations", "orphans", "--owner", "products", "--page-size", "1001"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid inspection command")
		}
	}
}

func TestTranslationsRescopeCommandRequiresExplicitApply(t *testing.T) {
	for _, args := range [][]string{{"translations", "rescope", "--owner", "products"}, {"translations", "rescope", "--owner", "products", "--apply"}} {
		if _, err := Parse(args, io.Discard); err != nil {
			t.Fatal(args, err)
		}
	}
	if _, err := Parse([]string{"translations", "orphans", "--owner", "products", "--apply"}, io.Discard); err == nil {
		t.Fatal("orphan inspection accepted a write flag")
	}
}
