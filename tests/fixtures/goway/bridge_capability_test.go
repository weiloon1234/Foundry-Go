package goway_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// K is inferred from a bridge signature even though the consumer cannot import
// its internal type. Merely hiding the type does not prevent a zero capability.
func openBridge[A, K, R any](fn func(A, K) R, a A) R                    { var k K; return fn(a, k) }
func openColumnBridge[A, K, C, P any](fn func(A, K) (C, P), a A) (C, P) { var k K; return fn(a, k) }
func applyBridge[A, B, K, R any](fn func(A, B, K) R, a A, b B) R        { var k K; return fn(a, b, k) }

func TestInferredZeroBridgeCapabilitiesFailClosed(t *testing.T) {
	requireInvalid := func(err error) {
		t.Helper()
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("expected invalid capability, got %v", err)
		}
	}
	q := models.Users.Query()
	age := expr.Value(orm.NumberExpr(models.Users.Age))
	source := openBridge(orm.FoundrySource[models.User], q)
	requireInvalid(source.Err)
	if len(source.Owners) != 0 || source.Node.Source.Name() != "" {
		t.Fatal("source metadata escaped")
	}
	c, p := openColumnBridge(orm.FoundryColumn[models.User, int, int], orm.ExpressionColumn[models.User, int, int](orm.NumberExpr(models.Users.Age)))
	requireInvalid(c.Err)
	requireInvalid(p.Err)
	if c.Bind != nil || c.Clone != nil || c.Scan != nil || c.Owner != nil || p.Bind != nil || p.Scan != nil {
		t.Fatal("column capabilities escaped")
	}
	unsealedCodec := openBridge(orm.FoundryCodec[int], orm.Codec(codec.Signed[int]()))
	requireInvalid(unsealedCodec.Validate())
	expression := openBridge(expr.FoundryExpression[models.User, int, int], age)
	requireInvalid(expression.Err)
	if expression.Node != nil || len(expression.Refs) != 0 {
		t.Fatal("expression metadata escaped")
	}
	selected := openBridge(expr.FoundrySelected[models.User, int64], expr.AsSelected(expr.Count[models.User]()))
	requireInvalid(selected.Err)
	if selected.Node != nil {
		t.Fatal("selected metadata escaped")
	}
	predicate := openBridge(expr.FoundryPredicate[models.User], expr.Eq(expr.Param(orm.NumberExpr(models.Users.Age), 1), 1))
	requireInvalid(predicate.Err)
	if predicate.Node != nil {
		t.Fatal("predicate metadata escaped")
	}
	order := openBridge(expr.FoundryOrder[models.User], expr.Asc(expr.Param(orm.NumberExpr(models.Users.Age), 1)))
	requireInvalid(order.Err)
	if order.Node.Expression != nil {
		t.Fatal("ordering metadata escaped")
	}
	type side struct{}
	aliased := rowset.As[side](q, "u")
	scope := openBridge(expr.FoundryScopeData[rowset.Alias[side, models.User], models.User], rowset.Scope(aliased))
	requireInvalid(scope.Err)
	if scope.Owner != nil || scope.Table != "" {
		t.Fatal("scope metadata escaped")
	}
	forgedScope := openBridge(expr.FoundryScope[models.User, models.User], scope)
	rejectedScope := expr.At(forgedScope, orm.NumberExpr(models.Users.Age))
	rejectedExpression := openBridge(expr.FoundryExpression[models.User, int, int], rejectedScope)
	requireInvalid(rejectedExpression.Err)
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	for _, bad := range []orm.Query[models.Order]{
		applyBridge(orm.FoundryWhere[models.Order], models.Orders.Query(), predicate),
		applyBridge(orm.FoundryOrderBy[models.Order], models.Orders.Query(), order),
	} {
		got, err := orm.All(t.Context(), db, bad)
		requireInvalid(err)
		if got != nil || capture.statements != 0 {
			t.Fatal("invalid bridge applied SQL", got, capture.statements)
		}
	}
	// Rejection does not mutate the original immutable query.
	if _, err := orm.Count(t.Context(), db, q); err != nil {
		t.Fatal("original query changed", err)
	}
	if capture.statements != 1 {
		t.Fatal(capture.statements)
	}
}

// The inferred K can implement the internal signature without importing it.
// Calling this structural implementation with an issued seal leaks authority.
type catcher[K any] struct {
	*database.DB
	calls        int
	captured     K
	transactions int
}

func catchObservers[K any](db *database.DB, _ func(K) (lifecycle.Observers, bool)) *catcher[K] {
	return &catcher[K]{DB: db}
}
func (c *catcher[K]) FoundryObservers(k K) (lifecycle.Observers, bool) {
	c.calls++
	c.captured = k
	return lifecycle.Observers{}, true
}
func (c *catcher[K]) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	c.transactions++
	return c.DB.Transaction(ctx, fn, options...)
}

type autocommitCatcher[K any] struct {
	*database.DB
	calls        int
	captured     K
	transactions int
}

func catchAutocommit[K any](db *database.DB, _ func(K, context.Context, string, ...any) (*database.Rows, error)) *autocommitCatcher[K] {
	return &autocommitCatcher[K]{DB: db}
}
func (c *autocommitCatcher[K]) FoundryAutocommitQuery(k K, _ context.Context, _ string, _ ...any) (*database.Rows, error) {
	c.calls++
	c.captured = k
	return nil, errors.New("untrusted autocommit implementation called")
}
func (c *autocommitCatcher[K]) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	c.transactions++
	return c.DB.Transaction(ctx, fn, options...)
}

func TestInferredCapabilityCatchersNeverReceiveAuthority(t *testing.T) {
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	createORM := func(writer database.Transactor) {
		t.Helper()
		id := must(model.ParseID[models.Order](orderID))
		buyer := must(model.ParseID[models.User](buyerID))
		row, err := orm.Insert(t.Context(), writer, models.Orders.Table, orm.KeySet(models.Orders.ID, id), orm.ColumnSet(models.Orders.BuyerID, buyer), orm.NumberSet(models.Orders.TotalCents, 500))
		if err != nil || row.ID != id {
			t.Fatal(row, err)
		}
	}
	// Exercise both native read and mutation observer lookup. A wrapper
	// may supply transactions but may not prove observer absence or receive a seal.
	observer := catchObservers(db, db.FoundryObservers)
	if _, err := orm.All(t.Context(), observer, models.Orders.Query()); err != nil {
		t.Fatal(err)
	}
	createORM(observer)
	if observer.calls != 0 || observer.transactions != 1 {
		t.Fatal("untrusted observer owner received authority", observer.calls, observer.transactions)
	}
	// Method values passed to autocommit helpers must also come from the concrete
	// DB/PrimaryExecutor switch, never from a structural wrapper assertion.
	autocommit := catchAutocommit(db, db.FoundryAutocommitQuery)
	createORM(autocommit)
	if autocommit.calls != 0 || autocommit.transactions != 1 {
		t.Fatal("untrusted autocommit owner received authority", autocommit.calls, autocommit.transactions)
	}
	if capture.statements != 3 {
		t.Fatal("unexpected statement count", capture.statements)
	}
}

func TestStage6a3InferredZeroCapabilitiesFailClosed(t *testing.T) {
	source, decode := openColumnBridge(orm.FoundryModel[models.User], models.Users.Query())
	if !errors.Is(source.Err, fault.Invalid) || decode != nil || len(source.Owners) != 0 {
		t.Fatal("model bridge exposed decoder/source", source.Err)
	}
	var aggregate expr.AggregateExpression[models.Order, value.Nullable[decimal.Decimal]] = expr.Sum(orm.NumberExpr(models.Orders.TotalCents))
	metadata, column := openColumnBridge(expr.FoundryAggregate[models.Order, value.Nullable[decimal.Decimal]], aggregate)
	if !errors.Is(metadata.Err, fault.Invalid) || !errors.Is(column.Err, fault.Invalid) || metadata.Node != nil || column.Scan != nil {
		t.Fatal("aggregate bridge exposed metadata/codec", metadata.Err, column.Err)
	}
	group := openBridge(expr.FoundryGroup[models.User], expr.Group(expr.Value(orm.NumberExpr(models.Users.Age))))
	key := openBridge(expr.FoundryKey[models.User], expr.Key(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))))))
	having := openBridge(expr.FoundryHaving[models.User], expr.GtValue(expr.Count[models.User](), 0))
	var order expr.ProjectionOrder[models.User] = expr.DescValue(expr.Count[models.User]())
	ordering := openBridge(expr.FoundryProjectionOrder[models.User], order)
	for _, err := range []error{group.Err, key.Err, having.Err, ordering.Err} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if group.Node != nil || key.Node != nil || having.Node != nil || ordering.Node.Expression != nil {
		t.Fatal("new bridge exposed metadata")
	}
}
