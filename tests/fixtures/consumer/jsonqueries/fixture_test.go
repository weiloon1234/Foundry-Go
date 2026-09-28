package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func document[T any](t *testing.T, input T) value.JSON[T] {
	t.Helper()
	v, err := value.NewJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func runJSON(t *testing.T, check func(*database.Tx, []jsonqueries.Document) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE json_policies (id jsonb PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE json_documents (id bigint PRIMARY KEY,settings jsonb NOT NULL,backup jsonb,tags jsonb NOT NULL,state jsonb NOT NULL,policy_id jsonb NOT NULL REFERENCES json_policies(id))`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		key := document(t, jsonqueries.PolicyKey{Version: 9007199254740993, Region: "apac"})
		if _, err := jsonqueries.QueryJsonPolicies().Create(t.Context(), tx, jsonqueries.PolicyDraft{}.SetID(key).SetName("primary")); err != nil {
			return err
		}
		var rows []jsonqueries.Document
		for i, prefs := range []jsonqueries.Preferences{{Theme: value.Set("dark")}, {}, {Theme: value.Set("dark")}} {
			tags := []string{"go", "db", "go"}
			if i == 1 {
				tags = []string{}
			}
			if i == 2 {
				tags = []string{"db", "go"}
			}
			draft := jsonqueries.DocumentDraft{}.SetID(i + 1).SetSettings(document(t, prefs)).SetTags(document(t, tags)).SetState(document(t, value.Null[string]())).SetPolicyID(key)
			if i == 0 {
				draft = draft.SetState(document(t, value.Of("ready")))
			}
			if i == 2 {
				draft = draft.SetBackup(document(t, prefs))
			}
			row, err := jsonqueries.QueryJsonDocuments().Create(t.Context(), tx, draft)
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return check(tx, rows)
	})
	if err != nil {
		t.Fatal(err)
	}
}
