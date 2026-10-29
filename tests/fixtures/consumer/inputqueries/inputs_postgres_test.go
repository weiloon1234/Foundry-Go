package inputqueries_test

import model "github.com/weiloon1234/Foundry-Go/model"

import (
	"errors"
	"testing"

	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func runInputs(t *testing.T, work func(*database.Tx) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE input_members(id uuid PRIMARY KEY,contact_email text NOT NULL,note text)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		return work(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMutationInputsPreserveHookAndStoredTypes(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		q := inputqueries.Members
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		raw := inputqueries.EmailInput{Address: " ADA@EXAMPLE.TEST "}
		draft := []orm.Assignment[inputqueries.Member]{orm.InputTextSet(inputqueries.Members.Email, raw), orm.NullableInputTextSet(inputqueries.Members.Note, inputqueries.LabelInput{Text: " note "})}
		member, err := orm.Insert(ctx, tx, q.Table, draft...)
		if err != nil {
			return err
		}
		beforeInput, ok := state.BeforeInput.Get()
		if !ok || beforeInput != raw || member.Email != "ada@example.test" || len(state.ORMCaptured) != 1 {
			return errors.New("create lost its typed input or stored output")
		}
		created, err := orm.ChangeOf(state.ORMCaptured[0], orm.InputTextExpr(q.Email))
		if err != nil {
			return err
		}
		stored, ok := created.After().Get()
		if !ok || stored != member.Email || !created.Assigned() || !created.Changed() || created.Before().IsSet() {
			return errors.New("create changes did not retain stored types")
		}
		state.Inspect = func(w *orm.Write[inputqueries.Member]) error {
			note, set := orm.WriteNullableGet(w, func() func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
				receiver := q.Note
				return func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
					return orm.NullableInputTextSet(receiver, arg0)
				}
			}()).Get()
			label, nonnull := note.Get()
			if orm.WriteIsSet(w, func() func(arg0 model.ID[inputqueries.Member]) orm.Assignment[inputqueries.Member] {
				receiver := q.Key()
				return func(arg0 model.ID[inputqueries.Member]) orm.Assignment[inputqueries.Member] {
					return orm.KeySet(receiver, arg0)
				}
			}()) || orm.WriteGet(w, func() func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
				receiver := q.Email
				return func(arg0 inputqueries.EmailInput) orm.Assignment[inputqueries.Member] {
					return orm.InputTextSet(receiver, arg0)
				}
			}()) != value.Set(raw) || !set || !nonnull || label.Text != " note " {
				return errors.New("create changed caller inputs or omissions")
			}
			return nil
		}
		if _, err := orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID, draft...); err != nil {
			return err
		}
		state.Inspect = nil
		if state.BeforeInput != value.Set(raw) {
			return errors.New("create changed the caller's draft")
		}
		found, err := orm.RequireKey(ctx, tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		link, err := found.AccessEmail()
		if err != nil {
			return err
		}
		if found.Email != member.Email || link != "mailto:ada@example.test" {
			return errors.New("read evaluated the getter or lost its explicit result")
		}

		state.ORMCaptured = nil
		member, err = orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: "  ADA@EXAMPLE.TEST  "}))
		if err != nil {
			return err
		}
		if len(state.ORMCaptured) != 1 {
			return errors.New("missing update changes")
		}
		emailChange, err := orm.ChangeOf(state.ORMCaptured[0], orm.InputTextExpr(q.Email))
		if err != nil {
			return err
		}
		noteChange, err := orm.NullableChangeOf(state.ORMCaptured[0], orm.NullableInputTextExpr(q.Note))
		if err != nil {
			return err
		}
		if !emailChange.Assigned() || emailChange.Changed() || noteChange.Assigned() {
			return errors.New("changes compared fresh input instead of stored values")
		}
		if note, ok := member.Note.Get(); !ok || note != "note" {
			return errors.New("omitted input changed the nullable field")
		}
		member, err = orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID, orm.NullableInputTextSetNull(inputqueries.Members.Note))
		if err != nil {
			return err
		}
		if !member.Note.IsNull() {
			return errors.New("input NULL became a scalar zero")
		}
		member, err = orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID, orm.NullableInputTextSet(inputqueries.Members.Note, inputqueries.LabelInput{}))
		if err != nil {
			return err
		}
		if note, ok := member.Note.Get(); !ok || note != "" {
			return errors.New("explicit zero input became NULL")
		}
		defaults, err := orm.Insert(ctx, tx, q.Table)
		if err != nil {
			return err
		}
		if defaults.Email != "default@example.test" || state.BeforeInput.IsSet() {
			return errors.New("before hook could not supply a missing typed input")
		}
		return nil
	})
}

func TestPostgresMutationInputFailuresRollBack(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		q := inputqueries.Members
		f := inputqueries.Members
		member, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: "original@example.test"}))
		if err != nil {
			return err
		}
		if _, err := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: "reject"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("input mutator veto was lost")
		}
		state := &inputqueries.State{VetoAfter: true}
		ctx := inputqueries.WithState(t.Context(), state)
		if _, err := orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: "pending@example.test"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("post-write veto was lost")
		}
		unchanged, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != member.Email {
			return errors.New("failed input write escaped its savepoint")
		}
		bad := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(rows.SetConflictValue(orm.InputTextExpr(f.Email), expr.Row(expr.At(rows.ConflictRowsOf(q.Query()).Proposed(), orm.InputTextExpr(f.Email))).Value().Param(inputqueries.StoredEmail("already stored"))))
		if _, err := orm.Upsert(t.Context(), tx, q.Query(), bad, orm.KeySet(inputqueries.Members.Key(), member.ID), orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: "fresh@example.test"})); !errors.Is(err, fault.Invalid) {
			return errors.New("explicit stored-value conflict bypassed the input boundary")
		}
		return nil
	})
}

func TestPostgresMutationInputBulkAndReusableConflict(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		q := inputqueries.Members
		member, err := orm.Insert(t.Context(), tx, q.Table)
		if err != nil {
			return err
		}
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		f := inputqueries.Members
		policy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(orm.ConflictSet(orm.InputTextExpr(f.Email), inputqueries.EmailInput{Address: " CONFLICT@EXAMPLE.TEST "}), orm.ConflictIncoming(orm.NullableInputTextExpr(f.Note)))
		draft := []orm.Assignment[inputqueries.Member]{orm.KeySet(inputqueries.Members.Key(), member.ID), orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: " PROPOSED@EXAMPLE.TEST "}), orm.NullableInputTextSet(inputqueries.Members.Note, inputqueries.LabelInput{Text: " bulk note "})}
		for range 2 {
			result, err := orm.Upsert(ctx, tx, q.Query(), policy, draft...)
			if err != nil {
				return err
			}
			updated, ok := result.Get()
			if !ok || updated.ID != member.ID || updated.Email != "conflict@example.test" {
				return errors.New("conflict did not transform fresh input")
			}
			if note, ok := updated.Note.Get(); !ok || note != "bulk note" {
				return errors.New("Incoming did not retain normalized stored output")
			}
		}
		members, err := orm.InsertMany(ctx, tx, q.Table, [][]orm.Assignment[inputqueries.Member]{
			[]orm.Assignment[inputqueries.Member]{orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: " FIRST@EXAMPLE.TEST "})},
			[]orm.Assignment[inputqueries.Member]{orm.InputTextSet(inputqueries.Members.Email, inputqueries.EmailInput{Address: " SECOND@EXAMPLE.TEST "}), orm.NullableInputTextSetNull(inputqueries.Members.Note)},
		})
		if err != nil {
			return err
		}
		emails := make(map[inputqueries.StoredEmail]bool, len(members))
		for _, member := range members {
			emails[member.Email] = true
		}
		if len(members) != 2 || !emails["first@example.test"] || !emails["second@example.test"] || len(state.ORMCaptured) != 0 {
			return errors.New("bulk input lost normalization or invoked per-model hooks")
		}
		return nil
	})
}
