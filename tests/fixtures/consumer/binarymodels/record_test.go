package binarymodels_test

import (
	"bytes"
	"testing"

	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestBinaryDraftAndSnapshotsOwnBuffers(t *testing.T) {
	input := binarymodels.Input{1, 2}
	raw := []byte{3}
	draft := binarymodels.Draft(1, input).SetRaw(raw).SetNote(input)
	input[0], raw[0] = 9, 9
	body, _ := draft.Body().Get()
	storedRaw, _ := draft.Raw().Get()
	note, _ := draft.Note().Get()
	nullable, _ := note.Get()
	if body[0] != 1 || storedRaw[0] != 3 || nullable[0] != 1 {
		t.Fatal("draft retained caller storage")
	}
	body[0], storedRaw[0], nullable[0] = 8, 8, 8
	body, _ = draft.Body().Get()
	storedRaw, _ = draft.Raw().Get()
	note, _ = draft.Note().Get()
	nullable, _ = note.Get()
	if body[0] != 1 || storedRaw[0] != 3 || nullable[0] != 1 {
		t.Fatal("draft getter exposed owned storage")
	}
	before := binarymodels.Record{ID: 1, Body: binarymodels.Payload{1}, Raw: []byte{}, Note: value.Of(binarymodels.Payload{2})}
	after := binarymodels.Record{ID: 1, Body: binarymodels.Payload{3}, Raw: []byte{}, Note: value.Null[binarymodels.Payload]()}
	change, err := binarymodels.BinaryChanges(before, after)
	if err != nil || !change.Fields().Body.Changed() || !change.Fields().Note.Changed() {
		t.Fatal("binary changes incorrect", err)
	}
	before.Body[0], after.Body[0] = 7, 7
	b, _ := change.Before().Get()
	a, _ := change.After().Get()
	if b.Body[0] != 1 || a.Body[0] != 3 {
		t.Fatal("model snapshot retained caller storage")
	}
	b.Body[0], a.Body[0] = 6, 6
	b, _ = change.Before().Get()
	a, _ = change.After().Get()
	if b.Body[0] != 1 || a.Body[0] != 3 {
		t.Fatal("model snapshot getter exposed owned storage")
	}
	field, _ := change.Fields().Body.Before().Get()
	field[0] = 5
	field, _ = change.Fields().Body.Before().Get()
	if !bytes.Equal(field, []byte{1}) {
		t.Fatal("field snapshot getter exposed owned storage")
	}
}
