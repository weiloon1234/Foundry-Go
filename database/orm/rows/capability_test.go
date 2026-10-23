package rows

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestScopeBridgeRejectsZeroCapabilityWithValidMetadata(t *testing.T) {
	m := declareMembers()
	source := As[left](m.Query(), "m")
	metadata := expr.FoundryScopeData(Scope(source), sqlowner.Expressions())
	var zero sqlowner.ExpressionSeal
	rejected := expr.FoundryScope[Alias[left, member], member](metadata, zero)
	actual := expr.FoundryScopeData(rejected, sqlowner.Expressions())
	if !errors.Is(actual.Err, fault.Invalid) || actual.Owner != nil || actual.Table != "" {
		t.Fatal("zero seal adopted scope", actual)
	}
}

func TestModelBridgesRejectZeroCapabilityWithValidMetadata(t *testing.T) {
	o := declareOrders()
	amount := expr.Value(o.MemberID)
	predicate := expr.FoundryPredicate(amount.Eq(1), sqlowner.Expressions())
	orderBy := expr.FoundryOrder(amount.Asc(), sqlowner.Expressions())
	var zero sqlowner.QuerySeal
	for _, q := range []orm.Query[order]{orm.FoundryWhere(o.Query(), predicate, zero), orm.FoundryOrderBy(o.Query(), orderBy, zero)} {
		state := &driverState{}
		got, err := q.All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("zero capability applied valid metadata", got, err, state.sql)
		}
	}
}
