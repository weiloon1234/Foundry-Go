package enum_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
)

type code uint64

type disguised int

func (disguised) MarshalJSON() ([]byte, error) { return []byte(`"1"`), nil }

type lossy int

func (lossy) MarshalJSON() ([]byte, error) { return []byte(`2`), nil }

func TestDescriptorsRejectChangedWireTypesAndValues(t *testing.T) {
	if err := enum.Describe("example.test/domain", "Disguised", enum.Case[disguised]{Name: "One", Value: 1}).Validate(); err == nil {
		t.Fatal("integer serialized as string")
	}
	if err := enum.Describe("example.test/domain", "Lossy", enum.Case[lossy]{Name: "One", Value: 1}).Validate(); err == nil {
		t.Fatal("wire value changed")
	}
}

func TestDescriptorOwnershipAndExactWireValues(t *testing.T) {
	cases := []enum.Case[code]{{Name: "Large", Value: 9007199254740993}}
	d := enum.Describe("example.test/domain", "Code", cases...)
	cases[0].Value = 0
	copy := d.Cases()
	copy[0].Name = "Changed"
	if !d.Contains(code(9007199254740993)) || d.Cases()[0].Name != "Large" {
		t.Fatal("descriptor ownership lost")
	}
	definition, err := d.Definition()
	if err != nil {
		t.Fatal(err)
	}
	if string(definition.Cases[0].Value) != "9007199254740993" {
		t.Fatal("integer precision lost")
	}
	definition.Cases[0].Value[0] = '0'
	fresh, err := d.Definition()
	if err != nil || !json.Valid(fresh.Cases[0].Value) || string(fresh.Cases[0].Value) != "9007199254740993" {
		t.Fatal("wire snapshot mutated descriptor")
	}
}

func TestInvalidDescriptors(t *testing.T) {
	for _, d := range []enum.Descriptor[code]{
		{}, enum.Describe[code]("", "Code", enum.Case[code]{Name: "One", Value: 1}),
		enum.Describe[code]("example.test/domain", "Code", enum.Case[code]{Name: "One", Value: 1}, enum.Case[code]{Name: "Two", Value: 1}),
		enum.Describe[code]("example.test/domain", "Code", enum.Case[code]{Name: "One", Value: 1}, enum.Case[code]{Name: "One", Value: 2}),
	} {
		if _, err := d.Definition(); err == nil {
			t.Fatal("accepted invalid declaration")
		}
	}
}
