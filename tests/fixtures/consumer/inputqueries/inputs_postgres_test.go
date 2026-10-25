package inputqueries_test

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
		draft := []orm.Assignment[inputqueries.Member]{inputqueries.Members.Email.Set(raw), inputqueries.Members.Note.Set(inputqueries.LabelInput{Text: " note "})}
		member, err := q.Insert(ctx, tx, draft...)
		if err != nil {
			return err
		}
		beforeInput, ok := state.BeforeInput.Get()
		if !ok || beforeInput != raw || member.Email != "ada@example.test" || len(state.ORMCaptured) != 1 {
			return errors.New("create lost its typed input or stored output")
		}
		created, err := orm.ChangeOf(state.ORMCaptured[0], q.Email)
		if err != nil {
			return err
		}
		stored, ok := created.After().Get()
		if !ok || stored != member.Email || !created.Assigned() || !created.Changed() || created.Before().IsSet() {
			return errors.New("create changes did not retain stored types")
		}
		state.Inspect = func(w *orm.Write[inputqueries.Member]) error {
			note, set := orm.WriteNullableGet(w, q.Note.Set).Get()
			label, nonnull := note.Get()
			if orm.WriteIsSet(w, q.Key().Set) || orm.WriteGet(w, q.Email.Set) != value.Set(raw) || !set || !nonnull || label.Text != " note " {
				return errors.New("create changed caller inputs or omissions")
			}
			return nil
		}
		if _, err := q.Update(ctx, tx, member.ID, draft...); err != nil {
			return err
		}
		state.Inspect = nil
		if state.BeforeInput != value.Set(raw) {
			return errors.New("create changed the caller's draft")
		}
		found, err := q.Require(ctx, tx, member.ID)
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
		member, err = q.Update(ctx, tx, member.ID, inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: "  ADA@EXAMPLE.TEST  "}))
		if err != nil {
			return err
		}
		if len(state.ORMCaptured) != 1 {
			return errors.New("missing update changes")
		}
		emailChange, err := orm.ChangeOf(state.ORMCaptured[0], q.Email)
		if err != nil {
			return err
		}
		noteChange, err := orm.NullableChangeOf(state.ORMCaptured[0], q.Note)
		if err != nil {
			return err
		}
		if !emailChange.Assigned() || emailChange.Changed() || noteChange.Assigned() {
			return errors.New("changes compared fresh input instead of stored values")
		}
		if note, ok := member.Note.Get(); !ok || note != "note" {
			return errors.New("omitted input changed the nullable field")
		}
		member, err = q.Update(ctx, tx, member.ID, inputqueries.Members.Note.SetNull())
		if err != nil {
			return err
		}
		if !member.Note.IsNull() {
			return errors.New("input NULL became a scalar zero")
		}
		member, err = q.Update(ctx, tx, member.ID, inputqueries.Members.Note.Set(inputqueries.LabelInput{}))
		if err != nil {
			return err
		}
		if note, ok := member.Note.Get(); !ok || note != "" {
			return errors.New("explicit zero input became NULL")
		}
		defaults, err := q.Insert(ctx, tx)
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
		member, err := q.Insert(t.Context(), tx, inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: "original@example.test"}))
		if err != nil {
			return err
		}
		if _, err := q.Update(t.Context(), tx, member.ID, inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: "reject"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("input mutator veto was lost")
		}
		state := &inputqueries.State{VetoAfter: true}
		ctx := inputqueries.WithState(t.Context(), state)
		if _, err := q.Update(ctx, tx, member.ID, inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: "pending@example.test"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("post-write veto was lost")
		}
		unchanged, err := q.Require(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != member.Email {
			return errors.New("failed input write escaped its savepoint")
		}
		bad := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(rows.SetConflictValue(f.Email, expr.Select(expr.At(rows.ConflictRowsOf(q.Query()).Proposed(), f.Email)).Param(inputqueries.StoredEmail("already stored"))))
		if _, err := orm.Upsert(t.Context(), tx, q.Query(), bad, inputqueries.Members.Key().Set(member.ID), inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: "fresh@example.test"})); !errors.Is(err, fault.Invalid) {
			return errors.New("explicit stored-value conflict bypassed the input boundary")
		}
		return nil
	})
}

func TestPostgresMutationInputBulkAndReusableConflict(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		q := inputqueries.Members
		member, err := q.Insert(t.Context(), tx)
		if err != nil {
			return err
		}
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		f := inputqueries.Members
		policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(orm.ConflictSet(f.Email, inputqueries.EmailInput{Address: " CONFLICT@EXAMPLE.TEST "}), orm.ConflictIncoming(f.Note))
		draft := []orm.Assignment[inputqueries.Member]{inputqueries.Members.Key().Set(member.ID), inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: " PROPOSED@EXAMPLE.TEST "}), inputqueries.Members.Note.Set(inputqueries.LabelInput{Text: " bulk note "})}
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
		members, err := q.InsertMany(ctx, tx, [][]orm.Assignment[inputqueries.Member]{
			[]orm.Assignment[inputqueries.Member]{inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: " FIRST@EXAMPLE.TEST "})},
			[]orm.Assignment[inputqueries.Member]{inputqueries.Members.Email.Set(inputqueries.EmailInput{Address: " SECOND@EXAMPLE.TEST "}), inputqueries.Members.Note.SetNull()},
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
