package enum_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
)

type privateWireError struct{}

func (*privateWireError) Error() string { return "private-enum-wire-value" }

var wireSentinel = &privateWireError{}

type failingWire int

func (failingWire) MarshalJSON() ([]byte, error) { return nil, wireSentinel }

type laterFailingWire int

var laterWireCalls int

func (laterFailingWire) MarshalJSON() ([]byte, error) {
	laterWireCalls++
	if laterWireCalls == 1 {
		return []byte("1"), nil
	}
	return nil, wireSentinel
}

func TestDescriptorRedactsWireErrorsAndPreservesCauses(t *testing.T) {
	d := enum.Describe("enum.test", "FailingWire", enum.Case[failingWire]{Name: "One", Value: 1})
	validateErr := d.Validate()
	_, definitionErr := d.Definition()
	laterWireCalls = 0
	late := enum.Describe("enum.test", "LaterFailingWire", enum.Case[laterFailingWire]{Name: "One", Value: 1})
	if err := late.Validate(); err != nil {
		t.Fatal(err)
	}
	_, lateErr := late.Definition()
	for _, err := range []error{validateErr, definitionErr, lateErr} {
		var private *privateWireError
		var marshaler *json.MarshalerError
		if !errors.Is(err, wireSentinel) || !errors.As(err, &private) || private != wireSentinel || !errors.As(err, &marshaler) {
			t.Fatal("enum validation lost the original marshaler error")
		}
		for _, diagnostic := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err), fmt.Errorf("descriptor failed: %w", err).Error()} {
			if strings.Contains(diagnostic, "private-enum-wire-value") {
				t.Fatal("enum diagnostic disclosed a private marshaler error")
			}
		}
	}
}
