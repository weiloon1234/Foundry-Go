package orm

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type insertTransferModel struct {
	ID      int64 `foundry:"default=database"`
	Name    string
	Enabled bool `foundry:"default=database"`
	Note    value.Nullable[string]
}
type insertTransferTable struct {
	Table[insertTransferModel, int64]
	Name    Text[insertTransferModel, string]
	Enabled Column[insertTransferModel, bool]
	Note    NullableText[insertTransferModel, string]
}

func insertTransferModels() insertTransferTable {
	return Define("users", func(b Builder[insertTransferModel], m *insertTransferModel) insertTransferTable {
		return insertTransferTable{TableOf(b, &m.ID), TextOf(b, &m.Name), ColumnOf(b, &m.Enabled), NullableTextOf(b, &m.Note)}
	})
}
func compileTransferBatch[M any](q Query[M], rows [][]Assignment[M], policy *Conflict[M]) (querycore.Statement, error) {
	p := writePlan{query: q.state, kind: querycore.InsertModel, batch: make([][]querycore.MutationAssignment, len(rows))}
	for i, row := range rows {
		item, err := planWrite(q.state, querycore.InsertModel, row, false)
		if err != nil {
			return querycore.Statement{}, err
		}
		p.batch[i] = item.assignments
	}
	if policy != nil {
		var err error
		p, err = p.withConflict(&policy.state)
		if err != nil {
			return querycore.Statement{}, err
		}
	}
	return p.compile()
}
func TestInsertBatchDefaultsAndConflictBindings(t *testing.T) {
	d := insertTransferModels()
	rows := [][]Assignment[insertTransferModel]{{TextSet(d.Name, "first"), ColumnSet(d.Enabled, false)}, {TextSet(d.Name, "second"), NullableTextSetNull(d.Note)}}
	policy := OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictIncoming(ColumnExpr(d.Enabled)), ConflictSet(NullableTextExpr(d.Note), "literal '; --")).Where(ColumnEq(d.Enabled, false))
	s, err := compileTransferBatch(d.Query(), rows, &policy)
	if err != nil || !strings.Contains(s.SQLText, `("name", "enabled", "note") VALUES ($1, $2, DEFAULT), ($3, DEFAULT, $4) ON CONFLICT ("name") DO UPDATE SET "enabled" = "excluded"."enabled", "note" = $5 WHERE ("foundry_upsert"."enabled" = $6) RETURNING "foundry_upsert"."id"`) || !reflect.DeepEqual(s.Bindings, []any{"first", false, "second", nil, "literal '; --", false}) {
		t.Fatal(s, err)
	}
	if strings.Contains(s.SQLText, "literal") {
		t.Fatal("literal interpolated")
	}
	derived := policy.Where(TextEq(d.Name, "extra")).DoUpdate(ConflictIncoming(NullableTextExpr(d.Note)))
	if len(policy.state.core.Condition) != 1 || len(policy.state.core.Updates) != 2 || len(derived.state.core.Condition) != 2 {
		t.Fatal("policy derivation mutated source")
	}
	for _, p := range []Conflict[insertTransferModel]{OnConflict[insertTransferModel]().DoNothing(), OnConflictConstraint[insertTransferModel]("users_name_key").DoNothing(), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictNull(NullableTextExpr(d.Note)))} {
		if _, err := compileTransferBatch(d.Query(), rows, &p); err != nil {
			t.Fatal(err)
		}
	}
	type defaults struct {
		ID int64 `foundry:"default=database"`
	}
	table := Define("defaults", func(b Builder[defaults], m *defaults) Table[defaults, int64] { return TableOf(b, &m.ID) })
	s, err = compileTransferBatch(table.Query(), [][]Assignment[defaults]{{}, {}}, nil)
	if err != nil || !strings.Contains(s.SQLText, `("id") VALUES (DEFAULT), (DEFAULT) RETURNING`) {
		t.Fatal("default batch", s, err)
	}
}
func TestInsertRejectsInvalidConflictAndEmptyBatchContracts(t *testing.T) {
	d, foreign := insertTransferModels(), insertTransferModels()
	unknown := d.Name
	core := *d.Name.text.core
	core.metadata.Name = "unknown"
	unknown.text.core = &core
	pretendNullable := d.Note
	nc := *d.Note.nullableText.base.core
	nc.metadata.Name = "name"
	pretendNullable.nullableText.base.core = &nc
	pretendNullable.nullableText.nullable = func() columnData[value.Nullable[string]] { c := d.Note.nullableText.nullable(); c.core = &nc; return c }
	valid := []Assignment[insertTransferModel]{TextSet(d.Name, "first")}
	for i, p := range []Conflict[insertTransferModel]{
		{}, OnConflict(ConflictKey(TextExpr(d.Name))), OnConflictConstraint[insertTransferModel]("").DoNothing(), OnConflictConstraint[insertTransferModel]("bad; --").DoNothing(),
		OnConflict(ConflictKey(TextExpr(d.Name)), ConflictKey(TextExpr(d.Name))).DoNothing(), OnConflict(ConflictKey(TextExpr(foreign.Name))).DoNothing(), OnConflict(ConflictKey(TextExpr(unknown))).DoNothing(), OnConflict(ConflictColumn[insertTransferModel]{}).DoNothing(),
		OnConflict[insertTransferModel]().DoUpdate(ConflictIncoming(TextExpr(d.Name))), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(),
		OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictIncoming(KeyExpr(d.Key()))), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictIncoming(TextExpr(d.Name)), ConflictIncoming(TextExpr(d.Name))), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictIncoming(TextExpr(foreign.Name))), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictUpdate[insertTransferModel]{}),
		OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictNull(NullableTextExpr(pretendNullable))), OnConflict(ConflictKey(TextExpr(d.Name))).DoNothing().Where(TextEq(d.Name, "invalid")), OnConflict(ConflictKey(TextExpr(d.Name))).DoUpdate(ConflictIncoming(TextExpr(d.Name))).Where(TextEq(foreign.Name, "invalid")),
	} {
		for _, rows := range [][][]Assignment[insertTransferModel]{{valid}, nil} {
			if _, err := compileTransferBatch(d.Query(), rows, &p); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid conflict", i, err)
			}
		}
	}
	if _, err := compileTransferBatch(d.Query(), [][]Assignment[insertTransferModel]{valid, {}}, nil); !errors.Is(err, fault.Missing) {
		t.Fatal("missing later row", err)
	}
	if _, err := compileTransferBatch(d.Query().Where(TextEq(d.Name, "scope")), nil, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty batch lost filters", err)
	}
	if s, err := compileTransferBatch(d.Query(), nil, nil); err != nil || s.SQLText != "" {
		t.Fatal("empty batch executable", s, err)
	}
}
