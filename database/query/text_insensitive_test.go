package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type literalTextRecord struct {
	ID   int64
	Name string
	Note value.Nullable[string]
}

func literalTextQuery() Query[literalTextRecord] {
	definition := Define("literal_text_records", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "note", Nullable: true}}, func(row database.Row) (literalTextRecord, error) {
		var result literalTextRecord
		err := row.Scan(codec.Signed[int64]().Scan(&result.ID), codec.String[string]().Scan(&result.Name), codec.Nullable(codec.String[string]()).Scan(&result.Note))
		return result, err
	})
	return ForModel(definition)
}

func literalTextCases() map[string]ValueQuery[literalTextRecord, int64] {
	q := literalTextQuery()
	id := NewOrderedField[literalTextRecord, int64]("literal_text_records", "id", codec.Signed[int64]())
	name := NewTextField[literalTextRecord, string]("literal_text_records", "name", codec.String[string]())
	note := NewNullableTextField[literalTextRecord, string]("literal_text_records", "note", codec.String[string]())
	selected := SelectValue(q, id.Value())
	grouped := selected.GroupBy(id.Group(), name.Group(), note.Group())
	return map[string]ValueQuery[literalTextRecord, int64]{
		"field":                 selected.Where(name.IContains("aLPHa %_!")),
		"nullable field":        selected.Where(note.IContains("aLPHa %_!")),
		"computed row":          selected.Where(Trim(name).IContains("aLPHa %_!")),
		"nullable computed row": selected.Where(TrimNullable(note).IContains("aLPHa %_!")),
		"group":                 grouped.Having(TextValue(name.Value()).IContains("aLPHa %_!")),
		"nullable group":        grouped.Having(TextNullableValue(note.Value()).IContains("aLPHa %_!")),
	}
}

func TestCaseInsensitiveContainsBindsLiteralWildcardsInEveryPhase(t *testing.T) {
	for name, query := range literalTextCases() {
		statement, err := query.Compile()
		if err != nil || !strings.Contains(statement.SQL(), " ILIKE $1 ESCAPE '!'") || !reflect.DeepEqual(statement.Arguments(), []any{"%aLPHa !%!_!!%"}) {
			t.Fatal("literal contains was changed into a SQL pattern", name, statement.SQL(), statement.Arguments(), err)
		}
	}
}

func TestPostgresCaseInsensitiveContainsKeepsWildcardsAndNullLiteral(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, statement := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE literal_text_records (id bigint PRIMARY KEY, name text NOT NULL, note text)`,
			`INSERT INTO literal_text_records VALUES (1,'Alpha %_! end','ALPHA %_! end'),(2,'alpha other',NULL),(3,'ALPHA %X! end','ALPHA %X! end')`,
		} {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		for name, query := range literalTextCases() {
			ids, err := query.All(t.Context(), tx)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(ids, []int64{1}) {
				t.Error("case folding, literal wildcard, or NULL semantics changed", name, ids)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
