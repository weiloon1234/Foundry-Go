package inputqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
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
		q := inputqueries.QueryInputMembers()
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		raw := inputqueries.EmailInput{Address: " ADA@EXAMPLE.TEST "}
		draft := inputqueries.MemberDraft{}.SetEmail(raw).SetNote(inputqueries.LabelInput{Text: " note "})
		member, err := q.Create(ctx, tx, draft)
		if err != nil {
			return err
		}
		beforeInput, ok := state.BeforeInput.Get()
		if !ok || beforeInput != raw || member.Email != "ada@example.test" || len(state.Captured) != 1 {
			return errors.New("create lost its typed input or stored output")
		}
		created := state.Captured[0].Fields().Email
		stored, ok := created.After().Get()
		if !ok || stored != member.Email || !created.Assigned() || !created.Changed() || created.Before().IsSet() {
			return errors.New("create changes did not retain stored types")
		}
		if input, _ := draft.Email().Get(); input != raw || draft.ID().IsSet() {
			return errors.New("create changed the caller's draft")
		}
		found, err := q.RequireFind(ctx, tx, member.ID)
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

		state.Captured = nil
		member, err = q.Update(ctx, tx, member.ID, inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: "  ADA@EXAMPLE.TEST  "}))
		if err != nil {
			return err
		}
		if len(state.Captured) != 1 {
			return errors.New("missing update changes")
		}
		fields := state.Captured[0].Fields()
		if !fields.Email.Assigned() || fields.Email.Changed() || fields.Note.Assigned() {
			return errors.New("changes compared fresh input instead of stored values")
		}
		if note, ok := member.Note.Get(); !ok || note != "note" {
			return errors.New("omitted input changed the nullable field")
		}
		member, err = q.Update(ctx, tx, member.ID, inputqueries.MemberDraft{}.ClearNote())
		if err != nil {
			return err
		}
		if !member.Note.IsNull() {
			return errors.New("input NULL became a scalar zero")
		}
		member, err = q.Update(ctx, tx, member.ID, inputqueries.MemberDraft{}.SetNote(inputqueries.LabelInput{}))
		if err != nil {
			return err
		}
		if note, ok := member.Note.Get(); !ok || note != "" {
			return errors.New("explicit zero input became NULL")
		}
		defaults, err := q.Create(ctx, tx, inputqueries.MemberDraft{})
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
		q := inputqueries.QueryInputMembers()
		f := inputqueries.MemberFields()
		member, err := q.Create(t.Context(), tx, inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: "original@example.test"}))
		if err != nil {
			return err
		}
		if _, err := q.Update(t.Context(), tx, member.ID, inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: "reject"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("input mutator veto was lost")
		}
		state := &inputqueries.State{VetoAfter: true}
		ctx := inputqueries.WithState(t.Context(), state)
		if _, err := q.Update(ctx, tx, member.ID, inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: "pending@example.test"})); !errors.Is(err, inputqueries.Veto) {
			return errors.New("post-write veto was lost")
		}
		unchanged, err := q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != member.Email {
			return errors.New("failed input write escaped its savepoint")
		}
		bad := query.OnConflict(f.ID).DoUpdate(f.Email.TextField.Set(inputqueries.StoredEmail("already stored")))
		if _, err := q.Upsert(t.Context(), tx, inputqueries.MemberDraft{}.SetID(member.ID).SetEmail(inputqueries.EmailInput{Address: "fresh@example.test"}), bad); !errors.Is(err, fault.Invalid) {
			return errors.New("explicit stored-value conflict bypassed the input boundary")
		}
		return nil
	})
}

func TestPostgresMutationInputBulkAndReusableConflict(t *testing.T) {
	runInputs(t, func(tx *database.Tx) error {
		q := inputqueries.QueryInputMembers()
		member, err := q.Create(t.Context(), tx, inputqueries.MemberDraft{})
		if err != nil {
			return err
		}
		state := &inputqueries.State{}
		ctx := inputqueries.WithState(t.Context(), state)
		f := inputqueries.MemberFields()
		policy := query.OnConflict(f.ID).DoUpdate(f.Email.Set(inputqueries.EmailInput{Address: " CONFLICT@EXAMPLE.TEST "}), f.Note.Incoming())
		draft := inputqueries.MemberDraft{}.SetID(member.ID).SetEmail(inputqueries.EmailInput{Address: " PROPOSED@EXAMPLE.TEST "}).SetNote(inputqueries.LabelInput{Text: " bulk note "})
		for range 2 {
			result, err := q.Upsert(ctx, tx, draft, policy)
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
		members, err := q.CreateMany(ctx, tx, []inputqueries.MemberDraft{
			inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: " FIRST@EXAMPLE.TEST "}),
			inputqueries.MemberDraft{}.SetEmail(inputqueries.EmailInput{Address: " SECOND@EXAMPLE.TEST "}).ClearNote(),
		})
		if err != nil {
			return err
		}
		emails := make(map[inputqueries.StoredEmail]bool, len(members))
		for _, member := range members {
			emails[member.Email] = true
		}
		if len(members) != 2 || !emails["first@example.test"] || !emails["second@example.test"] || len(state.Captured) != 0 {
			return errors.New("bulk input lost normalization or invoked per-model hooks")
		}
		return nil
	})
}
