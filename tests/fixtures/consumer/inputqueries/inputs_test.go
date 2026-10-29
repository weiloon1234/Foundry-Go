package inputqueries_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresMutationInputDraftKeepsTypesAndRedactsDiagnostics(t *testing.T) {
	const private = "PRIVATE INPUT MARKER"
	stop := errors.New("draft inspected")
	calls := 0
	state := &inputqueries.State{Inspect: func(draft *orm.Write[inputqueries.Member]) error {
		calls++
		var email value.Optional[inputqueries.EmailInput] = orm.WriteGet(draft, func() func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Email
			return func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
				return orm.InputTextSet(receiver, arg0)
			}
		}())
		var note value.Optional[value.Nullable[inputqueries.LabelInput]] = orm.WriteNullableGet(draft, func() func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Note
			return func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
				return orm.NullableInputTextSet(receiver, arg0)
			}
		}())
		if input, ok := email.Get(); !ok || input.Address != private {
			t.Fatal("draft discarded its typed input")
		}
		if input, ok := note.Get(); !ok || !input.IsNull() {
			t.Fatal("draft lost explicit NULL")
		}
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if text := fmt.Sprintf(format, draft); strings.Contains(text, private) || !strings.Contains(text, "orm write draft") {
				t.Fatal("draft diagnostics exposed values or lost their label")
			}
		}
		encoded, err := json.Marshal(draft)
		if err != nil || string(encoded) != "{}" {
			t.Fatal("draft serialized private inputs", err)
		}
		if err := orm.WriteUnset(draft, func() func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Email
			return func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
				return orm.InputTextSet(receiver, arg0)
			}
		}()); err != nil {
			return err
		}
		if err := orm.WriteUnset(draft, func() func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Note
			return func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
				return orm.NullableInputTextSet(receiver, arg0)
			}
		}()); err != nil {
			return err
		}
		if orm.WriteIsSet(draft, func() func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Email
			return func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
				return orm.InputTextSet(receiver, arg0)
			}
		}()) || orm.WriteIsSet(draft, func() func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
			receiver := inputqueries.Members.Note
			return func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
				return orm.NullableInputTextSet(receiver, arg0)
			}
		}()) {
			t.Fatal("input omission changed")
		}
		return stop
	}}
	runInputs(t, func(tx *database.Tx) error {
		_, err := orm.Insert(inputqueries.WithState(t.Context(), state), tx, inputqueries.Members.Table, orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: private}), orm.NullableInputTextSetNull(inputqueries.Members.Note))
		if !errors.Is(err, stop) || calls != 1 {
			t.Fatal("draft inspection failed", err, calls)
		}
		return nil
	})
}
