package http

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

type RoundTripHTTPKey struct{ text string }

func (k *RoundTripHTTPKey) UnmarshalText(text []byte) error { k.text = string(text); return nil }
func (k RoundTripHTTPKey) MarshalText() ([]byte, error) {
	switch k.text {
	case "panic":
		panic("private canonical key")
	case "goexit":
		runtime.Goexit()
	}
	return []byte(k.text), nil
}

type RoundTripKeyBody struct{ Values map[RoundTripHTTPKey]string }

func TestJSONKeyRoundTripKeepsInternalHTTPResponse(t *testing.T) {
	root := contract.TypeID("github.com/weiloon1234/Foundry-Go/http.RoundTripKeyBody")
	descriptor := contract.DefineJSON[RoundTripKeyBody](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "Values", Type: "map", Required: true}}},
		contract.JSONMapType[map[RoundTripHTTPKey]string]("map", "text", "key", contract.DefineJSONKey(contract.DefineScalar[RoundTripHTTPKey](contract.Type{ID: "github.com/weiloon1234/Foundry-Go/http.RoundTripHTTPKey", Kind: contract.StringKind}))),
		{ID: "text", Kind: contract.StringKind},
	}})
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "keys", Method: POST, Access: Public}, StaticPath("/")), EmptyQuery(), JSONBody(descriptor), EmptyResponse(204))
	calls := 0
	router, err := NewRouter(endpoint.Handle(func(context.Context, Input[NoPath, NoQuery, RoundTripKeyBody]) (NoContent, error) {
		calls++
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	control := httptest.NewRecorder()
	if err := WriteError(control, httptest.NewRequest("POST", "/", nil), InternalError); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"panic", "goexit", "healthy"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"Values":{"`+key+`":"value"}}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if key == "healthy" {
			if w.Code != 204 || calls != 1 {
				t.Fatal("healthy request failed after key codec failures", w.Code, calls)
			}
		} else if calls != 0 || w.Code != 500 || w.Body.String() != control.Body.String() {
			t.Fatal("key codec failure changed HTTP response", key, w.Code, w.Body.String())
		}
	}
}
