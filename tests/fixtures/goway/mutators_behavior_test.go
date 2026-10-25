package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"foundry.test/goway/coremodels"
	newmodels "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"strings"
	"testing"
)

func TestMutatorInputsAreReusableAndOwned(t *testing.T) {
	m := newmodels.Members
	capture := &goldenCapture{scriptedColumns: []string{"id", "contact_email", "note", "name", "body", "enabled"}, scriptedBatches: [][][]driver.Value{
		{{int64(1), "stored", nil, "default", []byte{1, 2}, true}},
		{{int64(1), "stored", nil, "default", []byte{1, 2}, true}},
	}}
	db := capture.open(t.Context())
	defer db.Close(context.Background())
	input := coremodels.BinaryInput{1, 2}
	assignment := m.Body.Set(input)
	input[0] = 9
	for range 2 {
		_, err := m.Insert(t.Context(), db, m.Key().Set(1), m.Email.Set(coremodels.EmailInput{Address: "stored"}), assignment)
		if err != nil {
			t.Fatal(err)
		}
	}
	if capture.statements != 2 {
		t.Fatal("reused input was not written twice")
	}
	before := capture.statements
	_, err := m.Update(t.Context(), db, 1, m.Email.Set(coremodels.EmailInput{Address: "reject"}))
	if !errors.Is(err, coremodels.MutatorVeto) || capture.statements != before {
		t.Fatal("mutator failure reached SQL", err)
	}
}

func TestMutatorErrorRedactionInBothAPIs(t *testing.T) {
	const private = "reject:private-email@example.test"
	// Exercise the retained runtime API without a generated draft or model.
	type legacyMember struct {
		ID    int64
		Email coremodels.StoredEmail
	}
	legacy := query.ForModel(query.Define("mutator_members", "id", []query.Column{{Name: "id"}, {Name: "contact_email"}},
		func(row database.Row) (legacyMember, error) {
			var item legacyMember
			err := row.Scan(codec.Signed[int64]().Scan(&item.ID), codec.String[coremodels.StoredEmail]().Scan(&item.Email))
			return item, err
		},
		query.NewModelField("id", codec.Signed[int64](), func(item legacyMember) int64 { return item.ID }),
		query.NewInputModelField("contact_email", codec.String[coremodels.StoredEmail](), func(item legacyMember) coremodels.StoredEmail { return item.Email }, coremodels.NormalizeEmail),
	))
	key := query.NewOrderedField[legacyMember]("mutator_members", "id", codec.Signed[int64]())
	for _, old := range []bool{true, false} {
		capture := &goldenCapture{}
		db := capture.open(t.Context())
		var err error
		if old {
			_, err = legacy.Where(key.Eq(1)).Patch(t.Context(), db, query.Change(query.AssignInput[legacyMember]("mutator_members", "contact_email", coremodels.EmailInput{Address: private})))
		} else {
			q := newmodels.Members
			_, err = q.Update(t.Context(), db, 1, q.Email.Set(coremodels.EmailInput{Address: private}))
		}
		var cause *coremodels.MutatorInputError
		if !errors.As(err, &cause) || cause.Value != private || !errors.Is(err, cause) {
			t.Fatal("mutator cause unavailable")
		}
		for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(text, private) {
				t.Fatal("private input in mutator error")
			}
		}
		if capture.statements != 0 {
			t.Fatal("failed mutator reached SQL")
		}
		if err := db.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
