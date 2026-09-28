package mailing_test

import (
	"errors"
	"foundry.test/consumer/mailing"
	"github.com/weiloon1234/Foundry-Go/contract"
	"testing"
)

func TestGeneratedRecipientUsesSharedEmailValueContract(t *testing.T) {
	descriptor := mailing.RecipientRequestJSON()
	limits := contract.JSONLimits{Bytes: 2048, Depth: 8, Nodes: 32, Steps: 64, Issues: 4}
	input, err := descriptor.Decode(t.Context(), []byte(`{"recipient":"User <user@example.test>"}`), limits)
	if err != nil || input.Recipient.Mailbox() != "user@example.test" {
		t.Fatal("generated address decode", err)
	}
	if _, err := descriptor.Encode(t.Context(), input, limits); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"recipient":42}`, `{"recipient":"invalid"}`, `{"recipient":"a@example.test\r\nBcc: bad@example.test"}`} {
		_, err := descriptor.Decode(t.Context(), []byte(raw), limits)
		var failure *contract.DecodeError
		if !errors.As(err, &failure) {
			t.Fatal("generated address validation bypassed", err)
		}
	}
	schema, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, typ := range schema.Types {
		if typ.ID == "github.com/weiloon1234/Foundry-Go/email.Address" {
			seen++
			if typ.Kind != contract.StringKind || typ.Format != "" {
				t.Fatal("address schema drift")
			}
		}
	}
	if seen != 1 {
		t.Fatal("address wire contract duplicated/omitted")
	}
}
