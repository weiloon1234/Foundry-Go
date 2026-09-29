package command

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
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

func TestVariantRegenerationCommandValidatesBeforeBoot(t *testing.T) {
	for _, args := range [][]string{
		{"attachments", "variants", "--owner", "members", "--collection", "photos"},
		{"attachments", "variants", "--owner", "members", "--collection", "photos", "--missing", "--batch", "100", "--format", "json"},
	} {
		if _, err := ParseVariants(args, io.Discard); err != nil {
			t.Fatal("valid variant command rejected", args, err)
		}
	}
	for _, args := range [][]string{
		{"attachments", "variants"},
		{"attachments", "variants", "--owner", "members"},
		{"attachments", "variants", "--owner", "members", "--collection", "Bad Name"},
		{"attachments", "variants", "--owner", "members", "--collection", "photos", "--batch", "0"},
		{"attachments", "variants", "--owner", "members", "--collection", "photos", "--batch", "101"},
		{"attachments", "variants", "--owner", "members", "--collection", "photos", "--format", "xml"},
		{"attachments", "variants", "--owner", "members", "--collection", "photos", "--delete"},
	} {
		if _, err := ParseVariants(args, io.Discard); err == nil {
			t.Fatal("invalid variant command accepted", args)
		}
	}
	if _, err := ParseVariants([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if err := (VariantCommand{}).Run(context.Background(), nil, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := VariantDeclaration(nil); err == nil {
		t.Fatal("declaration without a manager constructor accepted")
	}
}
