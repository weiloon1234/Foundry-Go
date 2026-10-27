package http

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type nestedChildPrivateError struct{ formatted atomic.Int32 }

func (e *nestedChildPrivateError) Error() string          { e.formatted.Add(1); panic("private nested error") }
func (e *nestedChildPrivateError) Format(fmt.State, rune) { _ = e.Error() }

type nestedHTTPChild struct{}

func (*nestedHTTPChild) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case `"panic"`:
		panic("private nested panic")
	case `"goexit"`:
		runtime.Goexit()
	case `"ordinary"`:
		return &nestedChildPrivateError{}
	}
	return nil
}

type NestedHTTPBody struct{}

func (*NestedHTTPBody) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	var child nestedHTTPChild
	first := jsonv2.UnmarshalDecode(dec, &child)
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	second := jsonv2.UnmarshalDecode(dec, &child)
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	var result string
	if err := jsonv2.UnmarshalDecode(dec, &result); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	switch result {
	case "first":
		return first
	case "join":
		return errors.Join(first, second)
	case "second":
		return second
	default:
		return nil
	}
}

func TestNestedJSONChildrenKeepHTTP500(t *testing.T) {
	root := contract.TypeID("github.com/weiloon1234/Foundry-Go/http.NestedHTTPBody")
	descriptor := contract.DefineJSON[NestedHTTPBody](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "first", Type: "text", Required: true}, {Name: "second", Type: "text", Required: true}, {Name: "result", Type: "text", Required: true}}}, {ID: "text", Kind: contract.StringKind},
	}})
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "nested", Method: POST, Access: Public}, StaticPath("/")), EmptyQuery(), JSONBody(descriptor), EmptyResponse(204))
	calls := 0
	router, err := NewRouter(endpoint.Handle(func(context.Context, Input[NoPath, NoQuery, NestedHTTPBody]) (NoContent, error) {
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
	for _, mode := range []string{"panic", "goexit"} {
		for _, result := range []string{"first", "join", "second", "handled"} {
			t.Run(mode+"/"+result, func(t *testing.T) {
				data := `{"first":"` + mode + `","second":"ordinary","result":"` + result + `"}`
				_, failure := descriptor.Decode(t.Context(), []byte(data), contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 100, Steps: 500, Issues: 8})
				if result == "handled" {
					if failure != nil {
						t.Fatal("handled children contaminated success")
					}
				} else {
					if failure == nil || errors.Is(failure, fault.Panicked) != (result != "second") {
						t.Fatal("returned graph lost or borrowed panic classification")
					}
					var cause *nestedChildPrivateError
					if errors.As(failure, &cause) != (result != "first") {
						t.Fatal("returned graph lost or borrowed ordinary cause")
					}
					_ = fmt.Sprintf("%v %+v %#v", failure, failure, failure)
					if cause != nil && cause.formatted.Load() != 0 {
						t.Fatal("application error formatted")
					}
				}
				req := httptest.NewRequest("POST", "/", strings.NewReader(data))
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				before := calls
				router.ServeHTTP(response, req)
				switch result {
				case "handled":
					if response.Code != 204 || calls != before+1 {
						t.Fatal("handled failures prevented success")
					}
				case "second":
					if response.Code != 400 || calls != before {
						t.Fatal("discarded panic contaminated ordinary failure")
					}
				default:
					if response.Code != 500 || response.Body.String() != control.Body.String() || calls != before {
						t.Fatal("nested panic changed HTTP bytes", response.Code, response.Body.String())
					}
				}
			})
		}
	}
}
