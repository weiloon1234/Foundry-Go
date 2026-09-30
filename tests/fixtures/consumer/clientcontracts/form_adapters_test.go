package clientcontracts_test

import (
	"path/filepath"
	"testing"

	"foundry.test/consumer/internal/clientfixture"
)

func TestOptionalFormAdaptersWithRealReactAndVue(t *testing.T) {
	tools := clientfixture.Load(t)
	source, err := fixture(t).Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tools.CheckFormAdapters(t, source, filepath.Join("testdata", "form_adapters"))
}
