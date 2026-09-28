package config_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/enum"
)

type scalarPort uint16
type mode string
type scalarSettings struct {
	Port  scalarPort
	Tiny  int8
	Huge  uint64
	Ratio float32
	Mode  mode
	Text  partialText
}
type partialText struct{ Value string }

func (p *partialText) UnmarshalText(data []byte) error {
	p.Value = string(data)
	if p.Value == "broken" {
		return errors.New("rejected text")
	}
	return nil
}

func TestScalarWidthsTextAndEnumMembership(t *testing.T) {
	portKey := config.Scalar("port", func(s *scalarSettings) *scalarPort { return &s.Port })
	tiny := config.Scalar("tiny", func(s *scalarSettings) *int8 { return &s.Tiny })
	huge := config.Scalar("huge", func(s *scalarSettings) *uint64 { return &s.Huge })
	ratio := config.Scalar("ratio", func(s *scalarSettings) *float32 { return &s.Ratio })
	selected := config.Enum("mode", func(s *scalarSettings) *mode { return &s.Mode }, enum.Describe("example.test/config", "Mode", enum.Case[mode]{Name: "Production", Value: "production"}))
	text := config.Text[scalarSettings, partialText, *partialText]("text", func(s *scalarSettings) *partialText { return &s.Text })
	schema, err := config.New(portKey, tiny, huge, ratio, selected, text)
	if err != nil {
		t.Fatal(err)
	}
	defaults := scalarSettings{Mode: "production"}
	values, _, err := schema.Load(defaults, config.Inputs[scalarSettings]{Files: []config.Values{{Name: "test", Data: map[string]string{"port": "65535", "tiny": "-128", "huge": "18446744073709551615", "ratio": "1.25", "text": "valid"}}}})
	if err != nil || values.Port != 65535 || values.Tiny != -128 || values.Huge != math.MaxUint64 || values.Ratio != 1.25 || values.Text.Value != "valid" {
		t.Fatalf("typed decode failed: %+v %v", values, err)
	}
	for key, invalid := range map[string][]string{"port": {"65536", "-1"}, "tiny": {"128", "-129"}, "huge": {"18446744073709551616"}, "ratio": {"NaN", "Inf", "1e100"}, "mode": {"other"}, "text": {"broken"}} {
		for _, raw := range invalid {
			got, report, err := schema.Load(defaults, config.Inputs[scalarSettings]{Files: []config.Values{{Name: "test", Data: map[string]string{key: raw}}}})
			if err == nil || got != (scalarSettings{}) || len(report.Entries()) != 0 {
				t.Fatalf("%s accepted invalid input or returned partial state", key)
			}
		}
	}
	for _, input := range []config.Inputs[scalarSettings]{{Overrides: []config.Override[scalarSettings]{selected.Set("other")}}, {Overrides: []config.Override[scalarSettings]{ratio.Set(float32(math.NaN()))}}} {
		if _, _, err := schema.Load(defaults, input); err == nil {
			t.Fatal("typed override bypassed value validation")
		}
	}
	if _, _, err := schema.Load(scalarSettings{Mode: "other"}, config.Inputs[scalarSettings]{}); err == nil {
		t.Fatal("invalid enum default accepted")
	}
	invalid := config.Enum("mode", func(s *scalarSettings) *mode { return &s.Mode }, enum.Descriptor[mode]{})
	if _, err := config.New(invalid); err == nil {
		t.Fatal("invalid enum declaration accepted")
	}
}

func TestScalarErrorsDoNotExposeInput(t *testing.T) {
	key := config.Scalar("port", func(s *scalarSettings) *scalarPort { return &s.Port }).Sensitive()
	schema, err := config.New(key)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = schema.Load(scalarSettings{}, config.Inputs[scalarSettings]{Files: []config.Values{{Name: "test", Data: map[string]string{"port": "credential-value"}}}})
	if err == nil || strings.Contains(err.Error(), "credential-value") {
		t.Fatal("input leaked or accepted")
	}
}
