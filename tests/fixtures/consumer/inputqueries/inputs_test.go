package inputqueries_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestMutationInputDraftKeepsTypesAndRedactsDiagnostics(t *testing.T) {
	const private = "PRIVATE INPUT MARKER"
	draft := inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: private}).ClearNote()
	var email value.Optional[inputqueries.EmailInput] = draft.Email()
	var note value.Optional[value.Nullable[inputqueries.LabelInput]] = draft.Note()
	if input, ok := email.Get(); !ok || input.Address != private {
		t.Fatal("draft discarded its typed input")
	}
	if input, ok := note.Get(); !ok || !input.IsNull() {
		t.Fatal("draft lost explicit NULL")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if text := fmt.Sprintf(format, draft); strings.Contains(text, private) || !strings.Contains(text, "Member draft") {
			t.Fatal("draft diagnostics exposed values or lost their label")
		}
	}
	encoded, err := json.Marshal(draft)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("draft serialized private inputs", err)
	}
	if !draft.UnsetEmail().UnsetNote().IsEmpty() {
		t.Fatal("input omission changed")
	}
}
