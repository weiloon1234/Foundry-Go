package orm

// OrderedExpressionColumn is the sealed constraint for ordered stored fields.
// Its private kind fields reject application wrappers that add ordered methods to
// enums/scalars, without adding a method to every column instantiation.
// Example: func minimum[M,V,P any,C orm.OrderedExpressionColumn[M,V,P]](c C).
type OrderedExpressionColumn[M, V, P any] interface {
	ExpressionColumn[M, V, P]
	~struct {
		_      [0]*M
		number columnData[V]
	} |
		~struct {
			_              [0]*M
			nullableNumber nullableColumnData[V]
		} |
		~struct {
			_    [0]*M
			text columnData[V]
		} |
		~struct {
			_            [0]*M
			nullableText nullableColumnData[V]
		} |
		~struct {
			_    [0]*M
			time columnData[V]
		} |
		~struct {
			_            [0]*M
			nullableTime nullableColumnData[V]
		} |
		~struct {
			_        [0]*M
			interval columnData[V]
		} |
		~struct {
			_                [0]*M
			nullableInterval nullableColumnData[V]
		}
}
