package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type embeddedTransactionInterface struct {
	TransactionScope
	ID int64
}
type embeddedTransactionConcrete struct {
	TransactionAlias[transactionTestAlias, transactionTestRecord]
	ID int64
}

type transactionMembershipInputs struct {
	value    TransactionValueSource[int64]
	nullable TransactionValueSource[value.Nullable[int64]]
}

func transactionJoinMembershipInputs[S TransactionScope](source TransactionProjectionSource[S]) transactionMembershipInputs {
	count := Count[S]().Value()
	return transactionMembershipInputs{SelectTransactionValue(source, count), SelectTransactionValue(source, Nullable(count))}
}

func TestEmbeddedTransactionMarkersCannotExecuteMembershipThroughPool(t *testing.T) {
	t.Run("interface", testTransactionMarkerMembership[embeddedTransactionInterface])
	t.Run("concrete", testTransactionMarkerMembership[embeddedTransactionConcrete])
}
func testTransactionMarkerMembership[M TransactionScope](t *testing.T) {
	outer := ForModel(Define("outer_records", "id", []Column{{Name: "id"}}, func(row database.Row) (M, error) { var result M; var id int64; return result, row.Scan(&id) }))
	field := NewOrderedField[M, int64]("outer_records", "id", codec.Signed[int64]())
	base := transactionTestModel()
	a := AsTransaction[transactionTestAlias](TransactionOf(base), "a")
	b := AsTransaction[transactionValueInner](TransactionOf(base), "b")
	left := NewOrderedField[TransactionAlias[transactionTestAlias, transactionTestRecord], int64]("a", "id", codec.Signed[int64]())
	right := NewOrderedField[TransactionAlias[transactionValueInner, transactionTestRecord], int64]("b", "id", codec.Signed[int64]())
	ordinary := NewOrderedField[transactionTestRecord, int64]("records", "id", codec.Signed[int64]())
	on := OnEqual(left, right)
	inputs := map[string]transactionMembershipInputs{
		"selected":   {SelectTransactionValue(a, Value(left)), SelectTransactionValue(a, Nullable(Value(left)))},
		"lifted":     {TransactionValueOf(SelectValue(base, Value(ordinary))), TransactionValueOf(SelectValue(base, Nullable(Value(ordinary))))},
		"inner_join": transactionJoinMembershipInputs(TransactionInnerJoin(a, b, on)),
		"left_join":  transactionJoinMembershipInputs(TransactionLeftJoin(a, b, on)),
		"right_join": transactionJoinMembershipInputs(TransactionRightJoin(a, b, on)),
		"full_join":  transactionJoinMembershipInputs(TransactionFullJoin(a, b, on)),
		"cross_join": transactionJoinMembershipInputs(TransactionCrossJoin(a, b)),
	}
	for name, input := range inputs {
		for variant, p := range map[string]Predicate[M]{"nonnullable": TransactionInQuery(field, input.value), "nullable": TransactionInNullableQuery(field, input.nullable)} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				q := outer.Where(p)
				for _, kind := range []readKind{readModels, readCount, readExists} {
					statement, err := q.compile(kind)
					if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQL() != "" {
						t.Fatal("embedded marker permitted ordinary compilation", statement, err)
					}
				}
				ex := &pageFailureExecutor{}
				if got, err := q.All(t.Context(), ex); got != nil || !errors.Is(err, fault.Invalid) {
					t.Fatal("embedded marker permitted execution", got, err)
				}
				if ex.calls != 0 {
					t.Fatal("transaction source reached executor", ex.calls)
				}
			})
		}
		t.Run(name+"/transaction", func(t *testing.T) {
			q := SelectTransactionValue(a, Value(left)).Where(TransactionInQuery(left, input.value), TransactionInNullableQuery(left, input.nullable))
			if _, err := q.Compile(); err != nil {
				t.Fatal("valid transaction composition rejected", err)
			}
		})
	}
}

func TestTransactionRecordIsStampedAtConstruction(t *testing.T) {
	base := transactionTestModel()
	ordinary := UnionAll(base, base)
	transaction := TransactionOf(ordinary)
	// Exercise the stored payload without any transaction getter. The public
	// conversion is separately rejected by the consumer compiler regression.
	escaped := SetQuery[transactionTestRecord]{record: transaction.record}
	alias := As[transactionTestAlias](escaped, "escaped")
	cte := As[transactionTestAlias](CTE("escaped_cte", escaped), "escaped")
	cursor := CursorFor(escaped)
	id := NewOrderedField[CursorScope[transactionTestRecord], int64](cursor.Scope().table, "id", codec.Signed[int64]())
	for name, compile := range map[string]func() (Statement, error){
		"set":    escaped.Compile,
		"alias":  SelectRecord(alias, alias.Scope()).Compile,
		"cte":    SelectRecord(cte, cte.Scope()).Compile,
		"cursor": cursor.UniqueBy(id.Group()).Compile,
	} {
		t.Run(name, func(t *testing.T) {
			statement, err := compile()
			if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQL() != "" {
				t.Fatal("stored transaction record escaped", statement, err)
			}
		})
	}
	ex := &pageFailureExecutor{}
	if got, err := escaped.All(t.Context(), ex); got != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal(got, err)
	}
	if ex.calls != 0 {
		t.Fatal("transaction record reached pool", ex.calls)
	}
	if _, err := ordinary.Compile(); err != nil {
		t.Fatal("transaction lift mutated original set", err)
	}
	a := AsTransaction[transactionTestAlias](transaction, "valid")
	if _, err := SelectTransactionRecord(a, a.Scope()).Compile(); err != nil {
		t.Fatal("transaction lift rejected valid execution", err)
	}
}
