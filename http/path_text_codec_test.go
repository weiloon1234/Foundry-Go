package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type typedPathFlag bool

func TestBooleanPathUsesCanonicalNamedValues(t *testing.T) {
	codec := BoolPath[typedPathFlag]()
	for _, value := range []typedPathFlag{true, false} {
		text, err := codec.Format(value)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := codec.Parse(text)
		if err != nil || parsed != value {
			t.Fatalf("boolean round trip: %v %v", parsed, err)
		}
	}
	for _, text := range []string{"", "1", "0", "True", "FALSE", " true"} {
		if _, err := codec.Parse(text); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid boolean %q: %v", text, err)
		}
	}
}

var invalidPathCode = errors.New("private text codec detail")

type pathCode string

func (v *pathCode) MarshalText() ([]byte, error) {
	if *v == "" {
		return nil, invalidPathCode
	}
	return []byte("code:" + string(*v)), nil
}
func (v *pathCode) UnmarshalText(data []byte) error {
	text, ok := strings.CutPrefix(string(data), "code:")
	if !ok || text == "" {
		*v = "partial"
		return invalidPathCode
	}
	*v = pathCode(text)
	return nil
}

func TestTextPathUsesPointerMethodsAndKeepsPrivateCauses(t *testing.T) {
	codec := TextPath[pathCode, *pathCode]()
	text, err := codec.Format(pathCode("a/b"))
	if err != nil || text != "code:a/b" {
		t.Fatalf("custom text encoding: %q %v", text, err)
	}
	got, err := codec.Parse(text)
	if err != nil || got != "a/b" {
		t.Fatalf("custom text decoding: %q %v", got, err)
	}
	got, err = codec.Parse("private-invalid-input")
	if got != "" || !errors.Is(err, invalidPathCode) || strings.Contains(err.Error(), "private") {
		t.Fatalf("failed decode retained partial state or exposed cause: %q %v", got, err)
	}
	if _, err := codec.Format(""); !errors.Is(err, invalidPathCode) || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe codec error: %v", err)
	}
}

type codecPath struct {
	Date   temporal.Date
	Amount decimal.Decimal
	Code   pathCode
}

func TestTextPathKeepsDecimalTemporalAndCustomValues(t *testing.T) {
	date, err := temporal.ParseDate("2026-09-14")
	if err != nil {
		t.Fatal(err)
	}
	amount, err := decimal.Parse("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	path := DefinePath("/reports/{date}/{amount}/{code}",
		Param("date", TextPath[temporal.Date, *temporal.Date](), func(p *codecPath) *temporal.Date { return &p.Date }),
		Param("amount", TextPath[decimal.Decimal, *decimal.Decimal](), func(p *codecPath) *decimal.Decimal { return &p.Amount }),
		Param("code", TextPath[pathCode, *pathCode](), func(p *codecPath) *pathCode { return &p.Code }),
	)
	route := DefineRoute(RouteSpec{ID: "reports.show", Method: GET, Access: Public}, path)
	input := codecPath{Date: date, Amount: amount, Code: "a/b"}
	location, err := route.URL(input)
	if err != nil {
		t.Fatal(err)
	}
	var received codecPath
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p codecPath) { received = p; w.WriteHeader(204) }))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
	if recorder.Code != 204 || received.Date.String() != date.String() || received.Amount.String() != amount.String() || received.Code != input.Code {
		t.Fatalf("typed text round trip failed: status %d", recorder.Code)
	}
}
