package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// The six declaration/computed/grouped paths must share literal escaping,
// independently of their nullable and selected-phase lowering.
func literalTextCases() map[string]Query[member, record] {
	m, r := declareMembers(), declareRows()
	selected := Select(From(m.Query()), r, Map(r.ID, expr.Value(orm.KeyExpr(m.ID))), Map(r.Name, expr.Value(orm.TextExpr(m.Name))))
	grouped := GroupBy(selected, expr.Group(expr.Value(orm.KeyExpr(m.ID))), expr.Group(expr.Value(orm.TextExpr(m.Name))), expr.Group(expr.Value(orm.NullableTextExpr(m.Nickname))))
	return map[string]Query[member, record]{
		"field":                 Select(From(m.Query().Where(orm.TextIContains(m.Name, "aLPHa %_!"))), r, Map(r.ID, expr.Value(orm.KeyExpr(m.ID))), Map(r.Name, expr.Value(orm.TextExpr(m.Name)))),
		"nullable field":        Select(From(m.Query().Where(orm.NullableTextIContains(m.Nickname, "aLPHa %_!"))), r, Map(r.ID, expr.Value(orm.KeyExpr(m.ID))), Map(r.Name, expr.Value(orm.TextExpr(m.Name)))),
		"computed row":          selected.Where(expr.Trim(expr.Value(orm.TextExpr(m.Name))).IContains("aLPHa %_!")),
		"nullable computed row": selected.Where(expr.TrimNullable(expr.Value(orm.NullableTextExpr(m.Nickname))).IContains("aLPHa %_!")),
		"group":                 Having(grouped, expr.TextValue(expr.Row(expr.Value(orm.TextExpr(m.Name))).Value()).IContains("aLPHa %_!")),
		"nullable group":        Having(grouped, expr.TextNullableValue(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))).Value()).IContains("aLPHa %_!")),
	}
}

func TestCaseInsensitiveContainsBindsLiteralWildcardsInEveryPhase(t *testing.T) {
	for name, q := range literalTextCases() {
		t.Run(name, func(t *testing.T) {
			s, err := CompileRead(Read(q))
			if err != nil || !strings.Contains(s.SQL(), " ILIKE $1 ESCAPE '!'") || !reflect.DeepEqual(s.Arguments(), []any{"%aLPHa !%!_!!%"}) {
				t.Fatal("literal became pattern", s.SQL(), s.Arguments(), err)
			}
		})
	}
}

func TestPostgresCaseInsensitiveContainsKeepsWildcardsAndNullLiteral(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `CREATE TABLE members (id bigint PRIMARY KEY, name text NOT NULL, nickname text)`, `INSERT INTO members VALUES (1,'Alpha %_! end','ALPHA %_! end'),(2,'alpha other',NULL),(3,'ALPHA %X! end','ALPHA %X! end')`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		for name, q := range literalTextCases() {
			got, err := Read(q).All(t.Context(), tx)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(got, []record{{1, "Alpha %_! end"}}) {
				t.Errorf("%s: literal/NULL semantics: %#v", name, got)
			}
		}
		m := declareMembers()
		for name, q := range map[string]Scalar[member, int64]{
			"pattern": SelectScalar(From(m.Query().Where(orm.NullableTextILike(m.Nickname, "%alpha %_!%"))), expr.Row(expr.Value(orm.KeyExpr(m.ID))).Value()),
			"null":    SelectScalar(From(m.Query().Where(orm.NullableTextIsNull(m.Nickname))), expr.Row(expr.Value(orm.KeyExpr(m.ID))).Value()),
		} {
			got, err := ReadScalar(OrderScalar(q, expr.Value(orm.KeyExpr(m.ID)).Asc())).All(t.Context(), tx)
			if err != nil {
				return err
			}
			want := []int64{1, 3}
			if name == "null" {
				want = []int64{2}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got %v want %v", name, got, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
