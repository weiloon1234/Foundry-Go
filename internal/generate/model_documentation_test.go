package generate

import (
	"strings"
	"testing"
)

func TestModelFieldDocumentation(t *testing.T) {
	dir := fixture(t, `package sample
//foundry:model table=records var=Records key=ID
type Record struct {
 // ID is the persisted identity.
 ID int64 `+"`foundry:\"member=ID\"`"+`
 // Name is the delivery label.
 Name string // Used for receipts.
}
`)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	got := generatedSnapshot(t, dir)["models_foundry.gen.go"]
	for _, text := range []string{"ID is the persisted identity.", "Name is the delivery label.", "Used for receipts.", "[Record.Name]"} {
		if !strings.Contains(got, text) {
			t.Errorf("generated member lost %q", text)
		}
	}
}
