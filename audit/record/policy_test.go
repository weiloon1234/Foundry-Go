package record_test

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCurrentRedactionNormalizesNamesAndExtendsTokens(t *testing.T) {
	for _, name := range []string{
		"passphrase", "otp", "otp_code", "pin", "PINs", "cvv2", "CVC", "ssn", "cardNumber", "card-numbers",
		"cookies", "session_id", "SessionToken", "recovery_codes", "RecoveryCode", "api_key_2", "apiKeys",
		"accesstoken", "userPasswords", "Authorization", "clientSecret1", "privateKeyPem", "credentials",
	} {
		if !record.SensitiveName(name) {
			t.Fatal("sensitive name was missed", name)
		}
	}
	for _, name := range []string{
		"id", "email", "tokenizer", "compass", "private_notes", "api_version", "spinner", "pinned",
		"passenger", "secretary", "keyboard", "opinion", "card_holder", "recovery_email", "sha256",
	} {
		if record.SensitiveName(name) {
			t.Fatal("unrelated name was redacted", name)
		}
	}
	if err := record.Redaction(0).Validate(); !errors.Is(err, fault.Invalid) || !record.Redaction(99).Sensitive("id") {
		t.Fatal("unknown redaction policy was trusted", err)
	}
}

// unsupportedPayload exercises rejection of retired model snapshot versions.
const unsupportedPayload = `{"version":1,"primary":"id","fields":[` +
	`{"name":"id","type":2,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"int","t":"7"}}},` +
	`{"name":"session_id","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"visible"}}}]}`

func TestUpdatesKeepOnlyTouchedFieldsWithoutEncodingOthers(t *testing.T) {
	encoded := 0
	counting := codec.New(func(v string) (driver.Value, error) { encoded++; return v, nil }, func(raw any) (string, error) {
		text, _ := raw.(string)
		return text, nil
	}).WithParameterType(codec.TypeText)
	b := builder(t, lifecycle.Update)
	untouched := changed(t, counting, value.Set("same"), value.Set("same"), false)
	encoded = 0
	if err := record.Capture(b, "untouched", counting, untouched, record.Automatic); err != nil {
		t.Fatal(err)
	}
	if encoded != 0 {
		t.Fatal("unchanged update field invoked its codec")
	}
	item := built(t, b)
	duplicate := builder(t, lifecycle.Update)
	if err := record.Capture(duplicate, "untouched", counting, untouched, record.Automatic); err != nil {
		t.Fatal(err)
	}
	if err := record.Capture(duplicate, "untouched", counting, untouched, record.Automatic); !errors.Is(err, fault.Duplicate) {
		t.Fatal("skipped fields bypassed duplicate detection", err)
	}
	if _, err := duplicate.Build(); !errors.Is(err, fault.Duplicate) {
		t.Fatal("ignored duplicate published a record", err)
	}
	field, err := record.ReadField(inspect(t, item), "untouched", counting)
	if err != nil || field.IsSet() {
		t.Fatal("update audit retained an untouched field", err)
	}
	created := builder(t, lifecycle.Create)
	if err := record.Capture(created, "untouched", counting, changed(t, counting, value.Optional[string]{}, value.Set("same"), false), record.Automatic); err != nil {
		t.Fatal(err)
	}
	field, err = record.ReadField(inspect(t, built(t, created)), "untouched", counting)
	if err != nil || !field.IsSet() {
		t.Fatal("creation audit omitted a complete snapshot field", err)
	}
	legacyAdd := builder(t, lifecycle.Update)
	if err := legacyAdd.Add(capture(t, "untouched", counting, untouched, record.Automatic)); err != nil {
		t.Fatal(err)
	}
	field, err = record.ReadField(inspect(t, built(t, legacyAdd)), "untouched", counting)
	if err != nil || field.IsSet() {
		t.Fatal("previously generated capture retained an untouched field", err)
	}
	if err := record.Capture(builder(t, lifecycle.Update), "x", counting, untouched, record.Disclosure(9)); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid disclosure accepted for a skipped field", err)
	}
}

func TestRetiredAndUnknownAuditPoliciesAreRejected(t *testing.T) {
	identity, err := model.NewReference[subject]("audited_subjects", int64(7), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0", "1", "3", "258"} {
		payload := strings.Replace(unsupportedPayload, `"version":1`, `"version":`+version, 1)
		if _, err := record.ParseEntry(identity, lifecycle.Create, payload); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsupported model snapshot accepted", version, err)
		}
	}
	type payload struct {
		Theme string `json:"theme"`
	}
	for _, policy := range []record.Redaction{0, 1, 3, 99} {
		if _, err := record.ParseDocumentWith[payload](policy, `{"theme":"dark"}`, false); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsupported document policy accepted", policy, err)
		}
		if err := policy.Validate(); !errors.Is(err, fault.Invalid) || !policy.Sensitive("theme") {
			t.Fatal("unsupported policy trusted", policy, err)
		}
	}
	current, err := record.ParseDocumentWith[payload](record.CurrentRedaction, `{"theme":"dark"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := current.Decode()
	if err != nil || decoded.Theme != "dark" {
		t.Fatal("current document did not round trip", err)
	}
	if _, err := record.ParseEntry(identity, lifecycle.Create, strings.Replace(unsupportedPayload, `"version":1`, `"version":2`, 1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("current policy accepted disclosed session data", err)
	}
}
