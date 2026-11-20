package inputqueries_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestORMDistinctInputDeclaration(t *testing.T) {
	if err := orm.Check(inputqueries.Members); err != nil {
		t.Fatal(err)
	}
	input := orm.Set(inputqueries.Members.Email, inputqueries.EmailInput{Address: "PRIVATE INPUT MARKER"})
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, input), "PRIVATE INPUT MARKER") {
			t.Fatal("assignment leaked input")
		}
	}
	if _, err := orm.Compile(inputqueries.Members.Query().Where(orm.Eq(inputqueries.Members.Email, inputqueries.StoredEmail("ada@example.test")))); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresORMDistinctInputsAndStoredChanges(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		raw := inputqueries.EmailInput{Address: " ADA@EXAMPLE.TEST "}
		member, err := orm.Insert(ctx, tx, inputqueries.Members.Table, orm.Set(inputqueries.Members.Email, raw), orm.Set(inputqueries.Members.Note, inputqueries.LabelInput{}))
		if err != nil {
			return err
		}
		if input, ok := state.BeforeInput.Get(); !ok || input != raw || member.Email != "ada@example.test" || member.Note != value.Of(inputqueries.StoredLabel("")) || len(state.ORMCaptured) != 1 {
			return errors.New("input or stored lifecycle values changed")
		}
		change, err := orm.ChangeOf(state.ORMCaptured[0], inputqueries.Members.Email)
		if err != nil {
			return err
		}
		if stored, ok := change.After().Get(); !ok || stored != member.Email || !change.Assigned() {
			return errors.New("capture lost stored value")
		}
		member, err = orm.Update(ctx, tx, inputqueries.Members.Table.Query(), inputqueries.Members.Table.Key(), member.ID, orm.SetNull(inputqueries.Members.Note))
		if err != nil {
			return err
		}
		if !member.Note.IsNull() || state.BeforeInput.IsSet() {
			return errors.New("NULL or omitted input changed")
		}
		state.VetoAfter = true
		if _, err = orm.Update(ctx, tx, inputqueries.Members.Table.Query(), inputqueries.Members.Table.Key(), member.ID, orm.Set(inputqueries.Members.Email, inputqueries.EmailInput{Address: "pending@example.test"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("veto identity lost")
		}
		current, err := orm.RequireKey(ctx, tx, inputqueries.Members.Table, member.ID)
		if err != nil {
			return err
		}
		if current.Email != member.Email {
			return errors.New("veto escaped savepoint")
		}
		return nil
	})
}
