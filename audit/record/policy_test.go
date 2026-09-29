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
	if record.RedactionV1.Sensitive("session_id") || !record.RedactionV1.Sensitive("password_hash") || !record.RedactionV2.Sensitive("session_id") {
		t.Fatal("redaction policy versions changed their frozen rules")
	}
	if err := record.Redaction(0).Validate(); !errors.Is(err, fault.Invalid) || !record.Redaction(99).Sensitive("id") {
		t.Fatal("unknown redaction policy was trusted", err)
	}
}

// legacyPayload is a history row written by the original policy: session_id
// was not sensitive and stayed disclosed.
const legacyPayload = `{"version":1,"primary":"id","fields":[` +
	`{"name":"id","type":2,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"int","t":"7"}}},` +
	`{"name":"session_id","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"visible"}}}]}`

func TestStoredHistoryIsValidatedUnderItsOwnRedactionPolicy(t *testing.T) {
	identity, err := model.NewReference[subject]("audited_subjects", int64(7), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacyPayload
	if codec.TypeText != 4 || codec.TypeInteger != 2 {
		t.Fatal("stored parameter type numbering changed")
	}
	entry, err := record.ParseEntry(identity, lifecycle.Create, legacy)
	if err != nil {
		t.Fatal("extending the redaction policy invalidated old history", err)
	}
	if entry.Redaction() != record.RedactionV1 {
		t.Fatal("legacy row lost its policy version")
	}
	view, err := record.Inspect(mustRestore(t, entry))
	if err != nil {
		t.Fatal(err)
	}
	field, err := record.ReadField(view, "session_id", codec.String[string]())
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := field.Get()
	// Validated under its own policy, then masked under the current one.
	if stored.After().State() != record.Redacted {
		t.Fatal("legacy value that the current policy treats as sensitive was disclosed")
	}
	current := strings.Replace(legacy, `"version":1`, `"version":2`, 1)
	if _, err := record.ParseEntry(identity, lifecycle.Create, current); !errors.Is(err, fault.Invalid) {
		t.Fatal("current policy accepted an unredacted session value", err)
	}
	oversized := strings.Replace(legacy, `"after":{"state":1,"value":{"k":"string","t":"visible"}}`, `"after":{"state":4,"size":7}`, 1)
	if _, err := record.ParseEntry(identity, lifecycle.Create, oversized); !errors.Is(err, fault.Invalid) {
		t.Fatal("legacy policy accepted a digest marker it never wrote", err)
	}
	future := strings.Replace(legacy, `"version":1`, `"version":3`, 1)
	if _, err := record.ParseEntry(identity, lifecycle.Create, future); !errors.Is(err, fault.Invalid) {
		t.Fatal("unknown future policy was accepted", err)
	}
}

func TestDomainDocumentsAreValidatedUnderTheirStoredPolicy(t *testing.T) {
	type payload struct {
		Session string `json:"session"`
	}
	text := `{"session":"legacy-visible"}`
	legacy, err := record.ParseDocumentWith[payload](record.RedactionV1, text, false)
	if err != nil {
		t.Fatal("old domain history became unreadable", err)
	}
	// The current policy masks the legacy session key on read.
	masked, err := legacy.Payload()
	if err != nil || masked != `{"session":"[redacted]"}` || !legacy.Redacted() || legacy.Redaction() != record.CurrentRedaction {
		t.Fatal("legacy document disclosed a currently sensitive key", masked, err)
	}
	if _, err := legacy.Decode(); !errors.Is(err, fault.Missing) {
		t.Fatal("masked legacy document decoded into a DTO", err)
	}
	if again, err := record.ParseDocumentWith[payload](legacy.Redaction(), masked, legacy.Redacted()); err != nil || !again.Redacted() {
		t.Fatal("masked document is not valid under its reported policy", err)
	}
	type plain struct {
		Theme string `json:"theme"`
	}
	unchanged, err := record.ParseDocumentWith[plain](record.RedactionV1, `{"theme":"dark"}`, false)
	if err != nil || unchanged.Redacted() || unchanged.Redaction() != record.RedactionV1 {
		t.Fatal("legacy document without sensitive keys changed", err)
	}
	if decoded, err := unchanged.Decode(); err != nil || decoded.Theme != "dark" {
		t.Fatal("legacy document did not decode", err)
	}
	if _, err := record.ParseDocument[payload](text, false); err == nil {
		t.Fatal("current policy accepted an unredacted session key")
	}
	captured, err := record.CaptureDocument(payload{Session: "hidden"})
	if err != nil || !captured.Redacted() || captured.Redaction() != record.CurrentRedaction {
		t.Fatal("current capture did not apply the extended policy", err)
	}
	if _, err := record.ParseDocumentWith[payload](record.Redaction(0), text, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("unknown document policy accepted", err)
	}
}

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

func mustRestore(t *testing.T, entry record.Entry) record.Model[subject, int64] {
	t.Helper()
	restored, err := record.RestoreModel(model.NewReference[subject]("audited_subjects", int64(0), codec.Signed[int64]()), entry)
	if err != nil {
		t.Fatal(err)
	}
	return restored
}

// legacyMasked is a policy-1 row whose otp, card_number and JSON keys the
// current policy treats as sensitive. Only password was sensitive then.
const legacyMasked = `{"version":1,"primary":"id","fields":[` +
	`{"name":"id","type":2,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"int","t":"7"}}},` +
	`{"name":"otp","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"123456"}}},` +
	`{"name":"card_number","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"4111111111111111"}}},` +
	`{"name":"note","type":4,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"visible"}}},` +
	`{"name":"profile","type":13,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":1,"value":{"k":"string","t":"{\"otp\":\"1\",\"theme\":\"dark\"}"}}},` +
	`{"name":"settings","type":13,"assigned":true,"changed":true,"before":{"state":0},"after":{"state":3,"value":{"k":"string","t":"{\"password\":\"[redacted]\",\"pin\":\"123456789012345\"}"}}}]}`

func TestLegacyHistoryIsMaskedUnderTheCurrentPolicyOnRead(t *testing.T) {
	identity, err := model.NewReference[subject]("audited_subjects", int64(7), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if codec.TypeJSON != 13 {
		t.Fatal("stored parameter type numbering changed")
	}
	entry, err := record.ParseEntry(identity, lifecycle.Create, legacyMasked)
	if err != nil {
		t.Fatal("legacy history became unreadable", err)
	}
	if entry.Redaction() != record.RedactionV1 {
		t.Fatal("legacy row lost its capturing policy")
	}
	view := inspect(t, mustRestore(t, entry))
	for name, want := range map[string]record.State{"otp": record.Redacted, "card_number": record.Redacted, "note": record.Disclosed} {
		field, err := record.ReadField(view, name, codec.String[string]())
		if err != nil {
			t.Fatal(err)
		}
		stored, _ := field.Get()
		if stored.After().State() != want {
			t.Fatal("legacy field has the wrong read state", name, stored.After().State())
		}
	}
	payload, err := entry.Payload()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"123456", "4111111111111111", `\"otp\":\"1\"`, "123456789012345"} {
		if strings.Contains(payload, secret) {
			t.Fatal("masked legacy payload still discloses a sensitive value", secret)
		}
	}
	if !strings.Contains(payload, `\"pin\":\"[redacted]\"`) || !strings.Contains(payload, "visible") {
		t.Fatal("masking removed more or less than the current policy requires", payload)
	}
	again, err := record.ParseEntry(identity, lifecycle.Create, payload)
	if err != nil || again.Redaction() != record.RedactionV1 {
		t.Fatal("masked entry is not valid under its capturing policy", err)
	}
	if repeated, err := again.Payload(); err != nil || repeated != payload {
		t.Fatal("masking is not idempotent", err)
	}
}
