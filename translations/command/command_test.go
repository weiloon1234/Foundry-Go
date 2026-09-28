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
