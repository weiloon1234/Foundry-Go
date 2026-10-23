package orm

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// conflictAssignment admits only framework column layouts with their original
// sealed metadata method. A defined application type cannot inherit ormColumn;
// an embedding wrapper cannot match these layouts. Thus Set is always the
// framework-owned capture, never an application override. Keeping this boundary
// here avoids adding methods to every column in ordinary read instantiations.
type conflictAssignment[M, I, V any] interface {
	ormColumn(*M) columnData[V]
	Set(I) Assignment[M]
	~struct {
		_      [0]*M
		column columnData[V]
	} |
		~struct {
			_   [0]*M
			key columnData[V]
		} |
		~struct {
			_      [0]*M
			number columnData[V]
		} |
		~struct {
			_    [0]*M
			text columnData[V]
		} |
		~struct {
			_    [0]*M
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
			_      [0]*M
			stored columnData[V]
			input  func(I) querycore.AssignmentValue
		} |
		~struct {
			_      [0]*M
			stored nullableColumnData[V]
			input  func(I) querycore.AssignmentValue
		} |
		~struct {
			_     [0]*M
			key   columnData[V]
			input func(I) querycore.AssignmentValue
		}
}
