package scalarqueries_test

import (
	"testing"

	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestComputedTextRowGuide(t *testing.T) {
	if _, err := rows.CompileRead(rows.Read(rows.FromModel(scalarqueries.NoteSearch("AL")))); err != nil {
		t.Fatal(err)
	}
}
