package fault_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestClassificationAndCauseWithoutFormattingSecrets(t *testing.T) {
	cause := errors.New("password=private")
	err := fmt.Errorf("configuration: %w", fault.Wrap(fault.Invalid, "invalid credential configuration", cause))
	if !errors.Is(err, cause) || !errors.Is(err, fault.Invalid) {
		t.Fatal("classification/cause was lost")
	}
	var detail *fault.Error
	if !errors.As(err, &detail) || detail.Code() != fault.Invalid {
		t.Fatal("typed error unavailable")
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "password=private") {
		t.Fatal("cause was exposed by formatting")
	}
	if strings.Contains(fmt.Sprintf("%#v", detail), "password=private") {
		t.Fatal("Go formatting exposed cause")
	}
}
