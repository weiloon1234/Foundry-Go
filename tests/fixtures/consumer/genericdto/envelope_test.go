package genericdto_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func TestGenericHTTPContractAndSharedInstances(t *testing.T) {
	router, err := genericdto.Router()
	if err != nil {
		t.Fatal(err)
	}
	combined, err := genericdto.CombinedJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Schemas: []contract.Schema{combined}})
	if err != nil {
		t.Fatal("standalone and nested identities disagree", err)
	}
	if _, err := openapi.Render(source, openapi.Options{Title: "Generic DTO", APIVersion: "1"}); err != nil {
		t.Fatal(err)
	}
	first, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := typescript.Render(source)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("SDK generation changed", err)
	}
	body := `{"id":"0193fd8c-2075-7000-8000-000000000001","name":"Typed user","note":null,"sequence":9223372036854775807}`
	request := httptest.NewRequest("POST", "/generic/echo", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var decoded genericdto.Envelope[genericdto.UserDTO]
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	note, present := decoded.Data.Note.Get()
	if decoded.Data.Name != "Typed user" || decoded.Trace != "fixture" || !present || !note.IsNull() || decoded.Data.Sequence != 9223372036854775807 {
		t.Fatal("generic HTTP value changed")
	}
	request = httptest.NewRequest("POST", "/generic/echo", strings.NewReader(strings.Replace(body, `"Typed user"`, `7`, 1)))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "/body/name") {
		t.Fatal("invalid generic input was not field-scoped", response.Code, response.Body.String())
	}
}

func BenchmarkGenericEnvelopeConstruction(b *testing.B) {
	element := genericdto.UserDTOJSON()
	b.Run("generic", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if genericdto.EnvelopeJSON(element).Validate() != nil {
				b.Fatal("invalid")
			}
		}
	})
	b.Run("concrete", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if genericdto.UserEnvelopeJSON().Validate() != nil {
				b.Fatal("invalid")
			}
		}
	})
}

func BenchmarkGenericEnvelopeEncoding(b *testing.B) {
	limits := contract.JSONLimits{Bytes: 4096, Depth: 8, Nodes: 100, Steps: 500, Issues: 8}
	user, err := genericdto.UserDTOJSON().Decode(b.Context(), []byte(`{"id":"0193fd8c-2075-7000-8000-000000000001","name":"example","sequence":7}`), limits)
	if err != nil {
		b.Fatal(err)
	}
	generic := genericdto.EnvelopeJSON(genericdto.UserDTOJSON())
	concrete := genericdto.UserEnvelopeJSON()
	b.Run("generic", func(b *testing.B) {
		v := genericdto.Envelope[genericdto.UserDTO]{Data: user, Trace: "bench"}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := generic.Encode(b.Context(), v, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("concrete", func(b *testing.B) {
		v := genericdto.UserEnvelope{Data: user, Trace: "bench"}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := concrete.Encode(b.Context(), v, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
}
