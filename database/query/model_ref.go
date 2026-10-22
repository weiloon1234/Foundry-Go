package query

// ModelRef identifies model M's generated metadata query for framework
// integrations. It must stay methodless, with non-generic state: Go compiles every
// method of each generic type reachable from a type's fields and method signatures
// in every importing package, so a Query[M] in ModelRef (or in a generated model's
// method set) would make every package that merely reads M compile M's query engine.
type ModelRef[M any] struct {
	_     [0]*M
	query any // func() Query[M]; see ModelQueryOf
}

// NewModelRef wraps M's generated query constructor. Generated models call it.
func NewModelRef[M any](query func() Query[M]) ModelRef[M] { return ModelRef[M]{query: query} }

// ModelQuery is implemented by every generated model.
type ModelQuery[M any] interface{ FoundryModel() ModelRef[M] }

// ModelQueryOf returns generated model M's metadata query. A zero or nil ModelRef
// yields the zero Query[M], which callers reject through their existing validation.
func ModelQueryOf[M ModelQuery[M]]() Query[M] {
	var model M
	build, _ := model.FoundryModel().query.(func() Query[M])
	if build == nil {
		return Query[M]{}
	}
	return build()
}
