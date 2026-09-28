package record_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type domainPayload struct {
	Names []string `json:"names"`
}

func TestDomainAuditOwnsCompletePayloadAndFreshDecode(t *testing.T) {
	input := domainPayload{Names: []string{"original"}}
	document, err := record.CaptureDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Names[0] = "mutated input"
	first, err := document.Decode()
	if err != nil || first.Names[0] != "original" {
		t.Fatal("capture did not own payload", err)
	}
	first.Names[0] = "mutated decode"
	second, err := document.Decode()
	if err != nil || second.Names[0] != "original" {
		t.Fatal("decode aliases stored history", err)
	}
	text, err := document.Payload()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := record.ParseDocument[domainPayload](text, false)
	if err != nil || restored.Redacted() {
		t.Fatal("restored complete payload changed", err)
	}
	if _, err := record.ParseDocument[domainPayload](text, true); err == nil {
		t.Fatal("false redaction flag accepted")
	}
	if _, err := record.ParseDocument[domainPayload](`{"wrong":1}`, false); err == nil {
		t.Fatal("restored DTO bypassed its schema")
	}
}

func TestDomainAuditRedactsNestedValuesWithoutFabricatingDTOs(t *testing.T) {
	type secrets struct {
		Token int `json:"apiToken"`
	}
	type payload struct {
		Nested []secrets `json:"nested"`
		Label  string    `json:"label"`
	}
	document, err := record.CaptureDocument(payload{Nested: []secrets{{Token: 123456789}}, Label: "public"})
	if err != nil || !document.Redacted() {
		t.Fatal("nested sensitive key was not redacted", err)
	}
	text, err := document.Payload()
	if err != nil || strings.Contains(text, "123456789") || !strings.Contains(text, "[redacted]") {
		t.Fatal("secret remained in payload", err)
	}
	if _, err := document.Decode(); !errors.Is(err, fault.Missing) {
		t.Fatal("redacted DTO was fabricated", err)
	}
	if _, err := record.ParseDocument[payload](text, true); err != nil {
		t.Fatal(err)
	}
	for _, corrupted := range []struct {
		text string
		flag bool
	}{
		{text, false}, {strings.Replace(text, `"[redacted]"`, `123456789`, 1), true},
	} {
		if _, err := record.ParseDocument[payload](corrupted.text, corrupted.flag); err == nil {
			t.Fatal("tampered redaction was accepted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", document, document), "public") {
		t.Fatal("routine formatting exposed data")
	}
}

type brokenPayload struct{ mode string }

var privateError = errors.New("private-domain-payload")

func (p brokenPayload) MarshalJSON() ([]byte, error) {
	switch p.mode {
	case "panic":
		panic(privateError)
	case "exit":
		runtime.Goexit()
	}
	return nil, privateError
}

func TestDomainAuditIsolatesCustomJSONFailures(t *testing.T) {
	for _, mode := range []string{"error", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			_, err := record.CaptureDocument(brokenPayload{mode: mode})
			if err == nil || strings.Contains(fmt.Sprintf("%v %#v", err, err), privateError.Error()) {
				t.Fatal("callback failure escaped or leaked")
			}
			if mode != "error" && !errors.Is(err, fault.Panicked) {
				t.Fatal("callback termination was not isolated", err)
			}
		})
	}
}
