package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"foundry.test/goway/coremodels"
	newmodels "foundry.test/goway/models/mutators"
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
	assignment := orm.InputColumnSet(m.Body, input)
	input[0] = 9
	for range 2 {
		_, err := orm.Insert(t.Context(), db, m.Table, orm.KeySet(m.Key(), 1), orm.InputTextSet(m.Email, coremodels.EmailInput{Address: "stored"}), assignment)
		if err != nil {
			t.Fatal(err)
		}
	}
	if capture.statements != 2 {
		t.Fatal("reused input was not written twice")
	}
	before := capture.statements
	_, err := orm.Update(t.Context(), db, m.Table.Query(), m.Table.Key(), 1, orm.InputTextSet(m.Email, coremodels.EmailInput{Address: "reject"}))
	if !errors.Is(err, coremodels.MutatorVeto) || capture.statements != before {
		t.Fatal("mutator failure reached SQL", err)
	}
}

func TestMutatorErrorRedaction(t *testing.T) {
	const private = "reject:private-email@example.test"
	{
		capture := &goldenCapture{}
		db := capture.open(t.Context())
		var err error
		q := newmodels.Members
		_, err = orm.Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 1, orm.InputTextSet(q.Email, coremodels.EmailInput{Address: private}))
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
