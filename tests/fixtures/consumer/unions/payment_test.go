package unions_test

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"foundry.test/consumer/genericdto"
	"foundry.test/consumer/unions"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func TestUnionConstructorsAccessorsAndVisitor(t *testing.T) {
	original := unions.CardDTO{Token: "test-token", Sequence: 9223372036854775807, Labels: []string{"owned"}}
	card, err := unions.PaymentMethodFromCard(original)
	if err != nil {
		t.Fatal(err)
	}
	original.Labels[0] = "changed"
	first, ok := card.Card()
	if !ok || first.Labels[0] != "owned" || card.Kind() != unions.PaymentMethodCardKind {
		t.Fatal("constructor lost ownership")
	}
	first.Labels[0] = "changed accessor"
	again, ok := card.Card()
	if !ok || again.Labels[0] != "owned" {
		t.Fatal("accessor shared snapshot")
	}
	if _, ok := card.Bank(); ok {
		t.Fatal("wrong variant accessor accepted")
	}
	if text, err := unions.Describe(card); err != nil || text != "test-token" {
		t.Fatal(text, err)
	}
	if _, err := unions.MatchPaymentMethod(card, func(unions.CardDTO) (string, error) { return "", nil }, nil, func(genericdto.Envelope[unions.CardDTO]) (string, error) { return "", nil }); err == nil {
		t.Fatal("nil unselected callback accepted")
	}
	var zero unions.PaymentMethod
	if !zero.IsZero() {
		t.Fatal("zero selected a variant")
	}
	if _, err := json.Marshal(zero); err == nil {
		t.Fatal("unset union encoded")
	}
	if _, err := unions.Describe(zero); err == nil {
		t.Fatal("unset union matched")
	}
	var nilReceiver *unions.PaymentMethod
	if nilReceiver.UnmarshalJSON([]byte(`{"kind":"bank_transfer","reference":"test"}`)) == nil {
		t.Fatal("nil receiver accepted")
	}
	before, _ := json.Marshal(card)
	if err := json.Unmarshal([]byte(`{"kind":"card","token":7,"sequence":1}`), &card); err == nil {
		t.Fatal("bad native payload accepted")
	}
	after, _ := json.Marshal(card)
	if string(before) != string(after) {
		t.Fatal("failed decode changed existing union")
	}
}

func TestUnionRealHTTPAndExporters(t *testing.T) {
	router, err := unions.Router()
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	data, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version < 2 {
		t.Fatal("union manifest is not versioned")
	}
	if _, err := manifest.Decode([]byte(strings.Replace(string(data), `"version": `+strconv.Itoa(manifest.Version), `"version": `+strconv.Itoa(manifest.Version-1), 1))); err == nil {
		t.Fatal("old manifest accepted")
	}
	api, err := openapi.Render(decoded, openapi.Options{Title: "Unions", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(api), `"discriminator"`) || !strings.Contains(string(api), `"oneOf"`) || !strings.Contains(string(api), `"const": "card"`) {
		t.Fatal("OpenAPI lost tags")
	}
	ts, err := typescript.Render(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ts), `readonly "kind": "card"`) {
		t.Fatal("TS lost discriminator")
	}
	payloads := []string{`{"kind":"card","token":"test","sequence":9223372036854775807}`, `{"kind":"bank_transfer","reference":"test-bank"}`, `{"kind":"wrapped","data":{"token":"test","sequence":1},"trace":"generic"}`}
	for _, payload := range payloads {
		body := `{"method":` + payload + `,"more":[` + payload + `],"maybe":null}`
		request := httptest.NewRequest("POST", "/unions/echo", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var result genericdto.Envelope[unions.PaymentRequest]
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		maybe, present := result.Data.Maybe.Get()
		if result.Data.Method.IsZero() || len(result.Data.More) != 1 || !present || !maybe.IsNull() || result.Trace != "union" {
			t.Fatal("nested union/presence lost")
		}
	}
	for _, payload := range []string{`{}`, `{"kind":"future","secret":"not-public"}`, `{"kind":7}`, `{"kind":"card","token":"test","sequence":1,"reference":"mixed"}`, `{"kind":"card","kind":"bank_transfer","reference":"duplicate"}`} {
		request := httptest.NewRequest("POST", "/unions/echo", strings.NewReader(`{"method":`+payload+`}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || strings.Contains(response.Body.String(), "not-public") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest("POST", "/unions/echo", strings.NewReader(`{"method":{"kind":7}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "/body/method/kind") {
		t.Fatal("private storage path leaked", response.Body.String())
	}
}

func BenchmarkUnionLifecycle(b *testing.B) {
	card := unions.CardDTO{Token: "fixture", Sequence: 9223372036854775807, Labels: []string{"a", "b"}}
	union, err := unions.PaymentMethodFromCard(card)
	if err != nil {
		b.Fatal(err)
	}
	descriptor := unions.PaymentMethodJSON()
	limits := contract.JSONLimits{Bytes: 4096, Depth: 8, Nodes: 100, Steps: 500, Issues: 8}
	raw, err := descriptor.Encode(b.Context(), union, limits)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("construct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := unions.PaymentMethodFromCard(card); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("accessor", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, ok := union.Card(); !ok {
				b.Fatal("missing")
			}
		}
	})
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := descriptor.Encode(b.Context(), union, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := descriptor.Decode(b.Context(), raw, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
}
