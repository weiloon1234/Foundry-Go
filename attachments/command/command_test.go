package command

import (
	"io"
	"testing"
)

func TestAttachmentInspectionRejectsMutationFlags(t *testing.T) {
	if _, err := Parse([]string{"attachments", "orphans", "--owner", "members"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"attachments", "orphans"}, {"attachments", "orphans", "--owner", "members", "--delete"}, {"attachments", "orphans", "--owner", "members", "--page-size", "1001"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("invalid inspection command accepted")
		}
	}
}
