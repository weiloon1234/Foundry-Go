package expr_test

import (
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type jsonEmbedded struct {
	Name string `json:"name"`
}
type jsonPayload struct {
	Count   int    `json:"count,string"`
	Ignored string `json:"-"`
	jsonEmbedded
}
type opaquePayload struct{ Name string }

func (opaquePayload) MarshalJSON() ([]byte, error) {
	panic("must never invoke codecs during declaration")
}

func TestJSONFieldStartupBinding(t *testing.T) {
	for _, err := range []error{
		expr.JSONFieldOf(func(p *jsonPayload) *int { return &p.Count }).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *string { return &p.Name }).Validate(),
		expr.JSONFixedArrayOf(func(p *[3]string) *string { return &p[1] }).Validate(),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	outside := "foreign"
	for _, err := range []error{
		expr.JSONField[jsonPayload, int]{}.Validate(),
		expr.JSONFieldOf[jsonPayload, int](nil).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *string { return &p.Ignored }).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *string { return &outside }).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *int { return nil }).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *int { panic("secret") }).Validate(),
		expr.JSONFieldOf(func(p *jsonPayload) *int { runtime.Goexit(); return nil }).Validate(),
		expr.JSONFieldOf(func(p *opaquePayload) *string { return &p.Name }).Validate(),
		expr.JSONFieldOf(func(p *struct {
			Name string `json:"name,unknown"`
		}) *string {
			return &p.Name
		}).Validate(),
		expr.JSONFieldOf(func(p *struct{ Name struct{} }) *struct{} { return &p.Name }).Validate(),
		expr.JSONFixedArray[[3]string, string]{}.Validate(),
		expr.JSONFixedArrayOf(func(p *[3]string) *string { return &outside }).Validate(),
		expr.JSONFixedArrayOf(func(p *jsonPayload) *int { return &p.Count }).Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("declaration did not fail closed: %v", err)
		}
	}
}
