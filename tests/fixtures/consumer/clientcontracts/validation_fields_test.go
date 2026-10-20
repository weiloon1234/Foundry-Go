package clientcontracts_test

import (
	"testing"

	"foundry.test/consumer/clientcontracts"
)

func TestLocalizedTitlesValidationField(t *testing.T) {
	if field := clientcontracts.PayloadValidationFields().Titles; field.Name() != "titles" {
		t.Fatal("localized titles field lost its JSON property name")
	}
}
