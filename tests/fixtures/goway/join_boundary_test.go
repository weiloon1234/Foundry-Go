package goway_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// The nil embedded interface lends only its marker. The shallower adapter
// supplies rowSource, so no method on that nil interface is invoked.
type borrowedSource[S any] struct{ rowset.Source[S] }
type wrappedInput[S any] struct {
	borrowedSource[S]
	rowset.Input[S]
}
type wrappedScalar[S, V any] struct {
	borrowedSource[S]
	rowset.Scalar[S, V]
}
type promotedInput[S any] struct{ wrappedInput[S] }
type promotedScalar[S, V any] struct{ wrappedScalar[S, V] }

type borrowedJoinTable[S any] struct{ rowset.JoinTable[S] }
type wrappedTableInput[S any] struct {
	borrowedJoinTable[S]
	rowset.Input[S]
}
type promotedTableInput[S any] struct{ wrappedTableInput[S] }
type promotedAlias[A, M any] struct{ rowset.Aliased[A, M] }

type boundaryCount struct{ Count int64 }
type boundaryFields struct {
	rowset.Row[boundaryCount]
	Count rowset.Field[boundaryCount, int64]
}

var boundaryRows = rowset.Define(func(b rowset.Builder[boundaryCount], r *boundaryCount) boundaryFields {
	return boundaryFields{b.Row(), rowset.NumberOf(b, &r.Count)}
})

func requireRejectedJoin[S any](t *testing.T, source rowset.Source[S]) {
	t.Helper()
	capture := &goldenCapture{}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	q := rowset.Select(source, boundaryRows, rowset.MapSelected(boundaryRows.Count, expr.Count[S]().Value()))
	got, err := q.All(t.Context(), db)
	if !errors.Is(err, fault.Invalid) || got != nil || capture.statements != 0 {
		t.Fatalf("unwrapped source reached execution: err=%v rows=%v statements=%d", err, got, capture.statements)
	}
}

func TestJoinRejectsBorrowedMarkersAndPromotedAdapters(t *testing.T) {
	q := models.Users.Query().Where(models.Users.Age.Gt(18)).Limit(1).Offset(1)
	input := rowset.From(q)
	scalar := rowset.SelectScalar(input, expr.Row(expr.Value(models.Users.Age)).Value())
	right := rowset.As[joinB](models.Users.Query(), "boundary_right")
	on := rowset.OnEqual(expr.Value(models.Users.Age), expr.At(rowset.Scope(right), models.Users.Age))
	for _, item := range []struct {
		name   string
		source rowset.Source[models.User]
	}{
		{"input", wrappedInput[models.User]{Input: input}},
		{"scalar", wrappedScalar[models.User, int]{Scalar: scalar}},
		{"promoted_input", promotedInput[models.User]{wrappedInput[models.User]{Input: input}}},
		{"promoted_scalar", promotedScalar[models.User, int]{wrappedScalar[models.User, int]{Scalar: scalar}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Run("left", func(t *testing.T) { requireRejectedJoin(t, rowset.LeftJoin(item.source, right, on)) })
			t.Run("right", func(t *testing.T) { requireRejectedJoin(t, rowset.RightJoin(item.source, right, on)) })
			t.Run("inner", func(t *testing.T) { requireRejectedJoin(t, rowset.InnerJoin(item.source, right, on)) })
			t.Run("full", func(t *testing.T) { requireRejectedJoin(t, rowset.FullJoin(item.source, right, on)) })
			t.Run("cross", func(t *testing.T) { requireRejectedJoin(t, rowset.CrossJoin(item.source, right)) })
		})
	}
}

func TestJoinRejectsBorrowedRightTableMarker(t *testing.T) {
	left := rowset.As[joinA](models.Users.Query(), "boundary_left")
	right := rowset.As[joinB](models.Users.Query(), "boundary_right")
	type rightScope = rowset.Alias[joinB, models.User]
	projected := rowset.Select(right, boundaryRows, rowset.MapSelected(boundaryRows.Count, expr.Count[rightScope]().Value()))
	input := rowset.FromRows(projected)
	on := rowset.OnEqual(expr.At(rowset.Scope(left), models.Users.Age), expr.At(rowset.Scope(right), models.Users.Age))
	for _, item := range []struct {
		name   string
		source rowset.JoinTable[rightScope]
	}{
		{"input", wrappedTableInput[rightScope]{Input: input}},
		{"promoted_input", promotedTableInput[rightScope]{wrappedTableInput[rightScope]{Input: input}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Run("left", func(t *testing.T) { requireRejectedJoin(t, rowset.LeftJoin(left, item.source, on)) })
			t.Run("right", func(t *testing.T) { requireRejectedJoin(t, rowset.RightJoin(left, item.source, on)) })
			t.Run("cross", func(t *testing.T) { requireRejectedJoin(t, rowset.CrossJoin(left, item.source)) })
		})
	}
}

func TestJoinAllowsPromotedAliasBoundary(t *testing.T) {
	left := rowset.As[joinA](models.Users.Query().Where(models.Users.Age.Gt(18)).Limit(1), "boundary_left")
	right := rowset.As[joinB](models.Users.Query(), "boundary_right")
	joined := rowset.RightJoin(promotedAlias[joinA, models.User]{left}, promotedAlias[joinB, models.User]{right}, rowset.OnEqual(expr.At(rowset.Scope(left), models.Users.Age), expr.At(rowset.Scope(right), models.Users.Age)))
	type joinedScope = rowset.Right[rowset.Alias[joinA, models.User], rowset.Alias[joinB, models.User]]
	capture := &goldenCapture{scriptedColumns: []string{"count"}, scriptedBatches: [][][]driver.Value{{{int64(2)}}}}
	db := capture.open(t.Context())
	defer db.Close(t.Context())
	got, err := rowset.Select(joined, boundaryRows, rowset.MapSelected(boundaryRows.Count, expr.Count[joinedScope]().Value())).All(t.Context(), db)
	if err != nil || len(got) != 1 || got[0].Count != 2 || capture.statements != 1 {
		t.Fatalf("valid promoted alias rejected: err=%v rows=%v statements=%d", err, got, capture.statements)
	}
}
