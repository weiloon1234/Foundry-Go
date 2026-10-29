package clientcontracts_test

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"testing"

	"foundry.test/consumer/clientcontracts"
)

func TestLocalizedTitlesValidationField(t *testing.T) {
	if field := clientcontracts.PayloadValidationFields().Titles; validation.FieldName(field) != "titles" {
		t.Fatal("localized titles field lost its JSON property name")
	}
}
