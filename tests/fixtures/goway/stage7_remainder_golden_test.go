package goway_test

import (
	"database/sql/driver"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"

	"fmt"
	"testing"

	"foundry.test/goway/integrations/seeding"

	"github.com/weiloon1234/Foundry-Go/database/seed"
)

func TestStage7SeederSQLParity(t *testing.T) {
	var transcripts []string
	{
		capture := &goldenCapture{scriptedColumns: []string{"code", "name", "enabled", "note", "amount"}, scriptedBatches: [][][]driver.Value{{{int64(1), "seeded", true, nil, "0"}}}}
		db := readDB(t, capture)
		definition := seeding.RecordSeeder("seeded")

		registry, err := seed.New(definition)
		if err != nil {
			t.Fatal(err)
		}
		result, err := registry.Run(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{result}))
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "stage7_remainder_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func TestStage7CursorMappingSQLParity(t *testing.T) {
	table := readUserTable()
	capture := &goldenCapture{scriptedColumns: readUserColumns, scriptedBatches: [][][]driver.Value{{readUser(buyerID, 21, nil), readUser(orderID, 22, nil)}}}
	db := readDB(t, capture)
	page, err := orm.CursorPaginate(t.Context(), db, table.Query(), orm.CursorRequest[cursorUser]{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := pagination.MapCursorPage(t.Context(), page, func(user cursorUser) (string, error) { return user.Email, nil })
	if err != nil {
		t.Fatal(err)
	}
	token, present := mapped.Next().Get()
	if !present {
		t.Fatal("missing navigation")
	}
	encoded, err := json.Marshal(mapped.Items())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(&capture.output, oracleSnapshot([]any{string(encoded), token.Token()}))
	if !oracleEqual(t, "stage7_remainder_golden/2", capture.output.String()) {
		t.Fatal("frozen cursor transport differs")
	}
}
