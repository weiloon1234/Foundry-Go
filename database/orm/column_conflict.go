package orm

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// Layouts plus sealed metadata methods admit concrete framework columns only.
type ordinaryLayouts[M, V any] interface {
	~struct {
		_      [0]*M
		column columnData[V]
	} |
		~struct {
			_      [0]*M
			role   [0]ordinaryRole
			number columnData[V]
		} |
		~struct {
			_    [0]*M
			role [0]ordinaryRole
			text columnData[V]
		} |
		~struct {
			_    [0]*M
			role [0]ordinaryRole
			time columnData[V]
		} |
		~struct {
			_    [0]*M
			enum columnData[V]
		} |
		~struct {
			_    [0]*M
			json columnData[V]
		} |
		~struct {
			_        [0]*M
			interval columnData[V]
		} |
		~struct {
			_              [0]*M
			nullableColumn nullableColumnData[V]
		} |
		~struct {
			_              [0]*M
			nullableNumber nullableColumnData[V]
		} |
		~struct {
			_            [0]*M
			nullableText nullableColumnData[V]
		} |
		~struct {
			role         [0]ordinaryRole
			_            [0]*M
			nullableTime nullableColumnData[V]
		} |
		~struct {
			_            [0]*M
			nullableEnum nullableColumnData[V]
		} |
		~struct {
			_            [0]*M
			nullableJSON nullableColumnData[V]
		} |
		~struct {
			_                [0]*M
			nullableInterval nullableColumnData[V]
		} |
		~struct {
			role         [0]deletedRole
			_            [0]*M
			nullableTime nullableColumnData[V]
		}
}
type createdLayouts[M, V any] interface {
	~struct {
		_    [0]*M
		role [0]createdRole
		time columnData[V]
	}
}
type primaryLayouts[M, V any] interface {
	~struct {
		_    [0]*M
		role [0]primaryRole
		key  columnData[V]
	} |
		~struct {
			_      [0]*M
			role   [0]primaryRole
			number columnData[V]
		} |
		~struct {
			_    [0]*M
			role [0]primaryRole
			text columnData[V]
		}
}

// InsertTarget admits unmutated stored assignments, creation timestamps and declared primary mappings.
// Example: rows.MapInsert(Archives.ID, expr.Value(Users.ID)).
type InsertTarget[M, V, P any] interface {
	ExpressionColumn[M, V, P]
	ordinaryLayouts[M, V] | createdLayouts[M, V] | primaryLayouts[M, V]
}

// UpdateTarget excludes all primary keys, mutators and managed update timestamps.
// Example: rows.MapUpdate(Users.Name, expr.Value(Archives.Name)).
type UpdateTarget[M, V, P any] interface {
	ExpressionColumn[M, V, P]
	ordinaryLayouts[M, V] | createdLayouts[M, V]
}

type conflictAssignment[M, I, V any] interface {
	ormColumn(*M) columnData[V]
	Set(I) Assignment[M]
	ordinaryLayouts[M, V] | createdLayouts[M, V] | primaryLayouts[M, V] |
		~struct {
			_    [0]*M
			role [0]lookupKeyRole
			key  columnData[V]
		} |
		~struct {
			_      [0]*M
			role   [0]lookupKeyRole
			number columnData[V]
		} |
		~struct {
			_    [0]*M
			role [0]lookupKeyRole
			text columnData[V]
		} |
		~struct {
			_    [0]*M
			role [0]updatedRole
			time columnData[V]
		} |
		~struct {
			_                  [0]*M
			inputColumnStorage columnData[V]
			input              func(I) querycore.AssignmentValue
		} |
		~struct {
			_                          [0]*M
			nullableInputColumnStorage nullableColumnData[V]
			input                      func(I) querycore.AssignmentValue
		} |
		~struct {
			_                [0]*M
			inputTextStorage columnData[V]
			input            func(I) querycore.AssignmentValue
		} |
		~struct {
			_                        [0]*M
			nullableInputTextStorage nullableColumnData[V]
			input                    func(I) querycore.AssignmentValue
		} |
		~struct {
			_                  [0]*M
			inputNumberStorage columnData[V]
			input              func(I) querycore.AssignmentValue
		} |
		~struct {
			_                          [0]*M
			nullableInputNumberStorage nullableColumnData[V]
			input                      func(I) querycore.AssignmentValue
		} |
		~struct {
			_                [0]*M
			inputTimeStorage columnData[V]
			input            func(I) querycore.AssignmentValue
		} |
		~struct {
			_                        [0]*M
			nullableInputTimeStorage nullableColumnData[V]
			input                    func(I) querycore.AssignmentValue
		} |
		~struct {
			_                [0]*M
			inputEnumStorage columnData[V]
			input            func(I) querycore.AssignmentValue
		} |
		~struct {
			_                        [0]*M
			nullableInputEnumStorage nullableColumnData[V]
			input                    func(I) querycore.AssignmentValue
		} |
		~struct {
			_                [0]*M
			inputJSONStorage columnData[V]
			input            func(I) querycore.AssignmentValue
		} |
		~struct {
			_                        [0]*M
			nullableInputJSONStorage nullableColumnData[V]
			input                    func(I) querycore.AssignmentValue
		} |
		~struct {
			_                    [0]*M
			inputIntervalStorage columnData[V]
			input                func(I) querycore.AssignmentValue
		} |
		~struct {
			_                            [0]*M
			nullableInputIntervalStorage nullableColumnData[V]
			input                        func(I) querycore.AssignmentValue
		} |
		~struct {
			_     [0]*M
			key   columnData[V]
			input func(I) querycore.AssignmentValue
		}
}
