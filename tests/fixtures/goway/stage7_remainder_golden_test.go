package goway_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"testing"

	"foundry.test/goway/integrations/seeding"
	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

func TestStage7SeederSQLParity(t *testing.T) {
	var transcripts []string
	for _, legacy := range []bool{true, false} {
		capture := &goldenCapture{scriptedColumns: []string{"code", "name", "enabled", "note", "amount"}, scriptedBatches: [][][]driver.Value{{{int64(1), "seeded", true, nil, "0"}}}}
		db := readDB(t, capture)
		definition := seeding.RecordSeeder("seeded")
		if legacy {
			definition.Run = func(ctx context.Context, tx *database.Tx) error {
				_, err := oldmodels.QueryWriteRecords().Create(ctx, tx, oldmodels.WriteRecordDraft{}.SetName("seeded"))
				return err
			}
		}
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
	if !oracleEqual(t, "stage7_remainder_golden/1", transcripts[0], transcripts[1]) {
		t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
	}
}

func TestStage7CursorMappingSQLParity(t *testing.T) {
	table := readUserTable()
	var transcripts []string
	for _, legacy := range []bool{true, false} {
		capture := &goldenCapture{scriptedColumns: readUserColumns, scriptedBatches: [][][]driver.Value{{readUser(buyerID, 21, nil), readUser(orderID, 22, nil)}}}
		db := readDB(t, capture)
		request := orm.CursorRequest[oldmodels.User]{Size: 1}
		var page orm.CursorPage[oldmodels.User]
		var err error
		if legacy {
			page, err = oldmodels.QueryUsers().CursorPaginate(t.Context(), db, query.CursorRequest[oldmodels.User](request))
		} else {
			page, err = orm.CursorPaginate(t.Context(), db, table.Query(), request)
		}
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := pagination.MapCursorPage(t.Context(), page, func(user oldmodels.User) (string, error) { return user.Email, nil })
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
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "stage7_remainder_golden/2", transcripts[0], transcripts[1]) {
		t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
	}
}
