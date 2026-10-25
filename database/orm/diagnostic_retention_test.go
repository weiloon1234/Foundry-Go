package orm_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestStatementRetentionRedaction(t *testing.T) {
	const secret = "statement-private-SECRET-945817"
	table := orm.Define("predicate_owners", func(b orm.Builder[predicateOwner], m *predicateOwner) orm.Table[predicateOwner, string] {
		return orm.TableOf(b, &m.ID)
	})
	scoped := table.Query().Where(table.Key().Eq(secret))
	statement, err := orm.Compile(scoped)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := rows.Compile(rows.SelectValue(scoped, table.Key()))
	if err != nil {
		t.Fatal(err)
	}
	frozenMatch(t, "statement", []any{statement.SQL(), statement.Arguments()})
	if !reflect.DeepEqual(projected.Arguments(), []any{secret}) {
		t.Fatal("projection bindings changed")
	}
	assertRetentionRedacted(t, "orm", statement, secret)
	assertRetentionRedacted(t, "orm/sql", statement, statement.SQL())
	assertRetentionRedacted(t, "rows", projected, secret)
	raw, err := orm.Compile(table.Query().Where(orm.WhereRaw[predicateOwner]("'"+secret+"' = ?", secret)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw.SQL(), secret) || !reflect.DeepEqual(raw.Arguments(), []any{secret}) {
		t.Fatal("explicit SQL inspection changed")
	}
	assertRetentionRedacted(t, "orm/raw-sql", raw, secret)
	binary, err := orm.FoundryStatement(querycore.Statement{SQLText: "SELECT $1", Bindings: []any{[]byte(secret)}}, sqlowner.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := binary.Arguments()
	snapshot[0].([]byte)[0] = '!'
	snapshot[0] = nil
	if !bytes.Equal(binary.Arguments()[0].([]byte), []byte(secret)) {
		t.Fatal("statement arguments alias returned snapshot")
	}
	assertRetentionRedacted(t, "orm/binary", binary, encodedDecimal(secret))
	var zero orm.Statement
	if zero.SQL() != "" || zero.Arguments() != nil {
		t.Fatal("zero statement inspection changed")
	}
}

type diagnosticInput struct{ Text string }
type diagnosticInputTable struct {
	orm.Table[predicateOwner, string]
	ID orm.InputKey[predicateOwner, diagnosticInput, string]
}

func TestAssignmentAndBoundRetentionRedaction(t *testing.T) {
	const secret = "assignment-input-SECRET-945817"
	table := orm.Define("predicate_owners", func(b orm.Builder[predicateOwner], m *predicateOwner) diagnosticInputTable {
		table := orm.TableOf(b, &m.ID)
		return diagnosticInputTable{table, orm.InputKeyOf(b, table.Key(), func(input diagnosticInput) (string, error) { return input.Text, nil })}
	})
	if err := orm.Check(table); err != nil {
		t.Fatal(err)
	}
	assertRetentionRedacted(t, "orm/stored", table.Key().Set(secret), secret)
	assertRetentionRedacted(t, "orm/input", table.ID.Set(diagnosticInput{secret}), secret)
	assertRetentionRedacted(t, "bound/maximum", orm.AtMost[int64](945817263), "945817263")
	assertRetentionRedacted(t, "bound/minimum", orm.AtLeast[int64](945817263), "945817263")
}

func encodedDecimal(s string) string { return strings.Trim(fmt.Sprintf("%v", []byte(s)), "[]") }
func encodedHex(s string) string {
	formatted := fmt.Sprintf("%#v", []byte(s))
	return strings.TrimSuffix(formatted[strings.IndexByte(formatted, '{')+1:], "}")
}

func TestExplainPlanRetentionRedaction(t *testing.T) {
	const secret = "plan-filter-SECRET-945817"
	raw := []byte(`[{"Plan":{"Node Type":"Seq Scan","Alias":"` + secret + `","Startup Cost":0,"Total Cost":1,"Plan Rows":1,"Plan Width":8,"Filter":"` + secret + `"}}]`)
	plan, err := querycore.DecodePlan(t.Context(), raw, false)
	if err != nil {
		t.Fatal(err)
	}
	var current orm.Plan = plan
	// Prove the encoded sentinels detect the unsafe representation, including
	// Go-syntax hex bytes; a plain substring assertion would miss this disclosure.
	unsafe := struct{ private []byte }{raw}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		sentinel := encodedDecimal(secret)
		if format == "%#v" {
			sentinel = encodedHex(secret)
		}
		if !strings.Contains(fmt.Sprintf(format, unsafe), sentinel) || !strings.Contains(fmt.Errorf("plan: "+format, unsafe).Error(), sentinel) {
			t.Fatal("encoded sentinel does not detect the inline byte representation")
		}
	}
	for _, sentinel := range []string{secret, encodedDecimal(secret), encodedHex(secret)} {
		assertRetentionRedacted(t, "orm/plan", current, sentinel)
	}
	if !bytes.Equal(current.JSON(), raw) || current.Root().Alias != secret || current.NodeCount() != 1 {
		t.Fatal("explicit plan inspection changed")
	}
	snapshot := current.JSON()
	snapshot[0] = '!'
	raw[0] = '!'
	if current.JSON()[0] != '[' {
		t.Fatal("plan JSON snapshot aliases retained bytes")
	}
	var zero orm.Plan
	if zero.JSON() != nil || zero.Root().Alias != "" || zero.NodeCount() != 0 || zero.PlanningMilliseconds().IsSet() {
		t.Fatal("zero plan inspection changed")
	}
}
