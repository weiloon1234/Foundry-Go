package rows

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type embeddedTransactionInterface struct {
	TransactionScopeType `foundry:"-"`
	ID                   int64
}
type embeddedTransactionConcrete struct {
	TransactionAlias[left, member] `foundry:"-"`
	ID                             int64
}

func TestEmbeddedTransactionMarkersCannotExecuteMembershipThroughPool(t *testing.T) {
	a := orm.Define("outer_members", func(b orm.Builder[embeddedTransactionInterface], m *embeddedTransactionInterface) orm.Table[embeddedTransactionInterface, int64] {
		return orm.TableOf(b, &m.ID)
	})
	b := orm.Define("outer_members", func(b orm.Builder[embeddedTransactionConcrete], m *embeddedTransactionConcrete) orm.Table[embeddedTransactionConcrete, int64] {
		return orm.TableOf(b, &m.ID)
	})
	t.Run("interface", func(t *testing.T) { testTransactionMarkerMembership(t, a) })
	t.Run("concrete", func(t *testing.T) { testTransactionMarkerMembership(t, b) })
}

type transactionMembershipInputs struct {
	value    TransactionValueSource[int64]
	nullable TransactionValueSource[value.Nullable[int64]]
}

func transactionJoinMembershipInputs[S TransactionScopeType](source TransactionSelectionSource[S]) transactionMembershipInputs {
	count := expr.Count[S]().Value()
	return transactionMembershipInputs{SelectTransactionValue(source, count), SelectTransactionValue(source, expr.Nullable(count))}
}

func testTransactionMarkerMembership[M TransactionScopeType](t *testing.T, outer orm.Table[M, int64]) {
	t.Helper()
	m := declareMembers()
	a := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "a")
	b := AsTransaction[right](TransactionOf(ModelRecord(m.Query())), "b")
	id := expr.Select(expr.At(TransactionScope(a), m.ID))
	on := On(expr.At(TransactionScope(a), m.ID), expr.At(TransactionScope(b), m.ID))
	ordinary := expr.Select(expr.Value(m.ID))
	inputs := map[string]transactionMembershipInputs{
		"selected":   {SelectTransactionValue(a, id), SelectTransactionValue(a, expr.Nullable(id))},
		"lifted":     {TransactionValueOf(Values(SelectScalar(From(m.Query()), ordinary))), TransactionValueOf(Values(SelectScalar(From(m.Query()), expr.Nullable(ordinary))))},
		"inner_join": transactionJoinMembershipInputs(TransactionInnerJoin(a, b, on)),
		"left_join":  transactionJoinMembershipInputs(TransactionLeftJoin(a, b, on)),
		"right_join": transactionJoinMembershipInputs(TransactionRightJoin(a, b, on)),
		"full_join":  transactionJoinMembershipInputs(TransactionFullJoin(a, b, on)),
		"cross_join": transactionJoinMembershipInputs(TransactionCrossJoin(a, b)),
	}
	state := &driverState{columns: []string{"id"}}
	db := openTest(t, state)
	for name, input := range inputs {
		for variant, p := range map[string]expr.Predicate[M]{
			"nonnullable": TransactionInQuery(expr.Value(outer.Key()), input.value),
			"nullable":    TransactionInNullableQuery(expr.Value(outer.Key()), input.nullable),
		} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				q := expr.Where(outer.Query(), p)
				if got, err := q.All(t.Context(), db); got != nil || !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
					t.Fatal("embedded marker permitted pool execution", got, err)
				}
				if _, err := q.Count(t.Context(), db); !errors.Is(err, fault.Invalid) {
					t.Fatal("count escaped transaction", err)
				}
				if _, err := q.Exists(t.Context(), db); !errors.Is(err, fault.Invalid) {
					t.Fatal("exists escaped transaction", err)
				}
				if state.sql != "" {
					t.Fatal("transaction source reached pool", state.sql)
				}
			})
		}
	}
	// A valid transaction selection still accepts the same unlocked operands.
	for name, input := range inputs {
		t.Run(name+"/transaction", func(t *testing.T) {
			q := SelectTransactionValue(a, id).Where(TransactionInQuery(expr.At(TransactionScope(a), m.ID), input.value), TransactionInNullableQuery(expr.At(TransactionScope(a), m.ID), input.nullable))
			compileTransaction(t, q.core.Source)
		})
	}
}

func TestTransactionRecordIsStampedAtConstruction(t *testing.T) {
	m := declareMembers()
	original := ModelRecord(m.Query())
	lifted := TransactionOf(original)
	if !lifted.core.Source.Node.Source.RequiresTransaction {
		t.Fatal("transaction record has an unrestricted stored source")
	}
	if original.core.Source.Node.Source.RequiresTransaction {
		t.Fatal("transaction lift mutated ordinary record")
	}
}
