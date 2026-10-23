package orm

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"
)

const codecPath = "github.com/weiloon1234/Foundry-Go/database/codec"
const ormPath = "github.com/weiloon1234/Foundry-Go/database/orm"
const ormcorePath = "github.com/weiloon1234/Foundry-Go/internal/ormcore"

type codecBody struct {
	name       string
	parameters []*types.TypeParam
	families   []map[string]bool
	edges      []codecEdge
}
type codecEdge struct {
	target string
	// For each callee parameter, the caller parameters appearing in its argument.
	arguments [][]int
}

func codecFunctionName(f *types.Func) string {
	return f.Pkg().Path() + "." + f.FullName()
}

func codecParameters(s *types.Signature) []*types.TypeParam {
	var result []*types.TypeParam
	for _, list := range []*types.TypeParamList{s.TypeParams(), s.RecvTypeParams()} {
		for i := 0; i < list.Len(); i++ {
			result = append(result, list.At(i))
		}
	}
	return result
}

// Walk instantiated type arguments, retaining V even inside Nullable[V] or []V.
func codecArgumentParameters(typ types.Type, parameters []*types.TypeParam) []int {
	seen := map[int]bool{}
	var walk func(types.Type)
	walk = func(typ types.Type) {
		switch t := types.Unalias(typ).(type) {
		case *types.TypeParam:
			for i, p := range parameters {
				if types.Identical(t, p) {
					seen[i] = true
				}
			}
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				walk(t.TypeArgs().At(i))
			}
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Key())
			walk(t.Elem())
		case *types.Chan:
			walk(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				walk(t.Field(i).Type())
			}
		case *types.Signature:
			for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
				for i := 0; i < tuple.Len(); i++ {
					walk(tuple.At(i).Type())
				}
			}
		}
	}
	walk(typ)
	var result []int
	for i := range parameters {
		if seen[i] {
			result = append(result, i)
		}
	}
	return result
}

// Inspect resolved function references, including inferred instantiations and
// function values, not just call syntax. Summaries propagate to a fixed point
// across package boundaries so helper names and helper depth cannot hide costs.
func codecBodyViolations(files []*ast.File, info *types.Info, bodies map[string]*codecBody) []string {
	families := map[string]bool{"Signed": true, "Unsigned": true, "Float": true, "String": true, "Bool": true, "ID": true, "Decimal": true, "Time": true, "DateTime": true, "Date": true, "WallTime": true, "LocalDateTime": true, "Bytes": true}
	var bad []string
	for _, file := range files {
		for _, decl := range file.Decls {
			f, ok := decl.(*ast.FuncDecl)
			if !ok || f.Body == nil {
				continue
			}
			object := info.Defs[f.Name].(*types.Func)
			parameters := codecParameters(object.Type().(*types.Signature))
			if len(parameters) == 0 {
				continue
			}
			body := &codecBody{name: codecFunctionName(object), parameters: parameters, families: make([]map[string]bool, len(parameters))}
			for i := range body.families {
				body.families[i] = map[string]bool{}
			}
			bodies[body.name] = body
			ast.Inspect(f.Body, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				target, ok := info.Uses[id].(*types.Func)
				if !ok || target.Pkg() == nil {
					return true
				}
				path, name := target.Pkg().Path(), target.Name()
				// New can encode an inline representation adapter too. Only the two
				// audited pointer shells may create a generic codec in these packages.
				adapter := path == codecPath && (name == "Adapt" || name == "New")
				shell := object.Pkg().Path() == ormcorePath && (object.Name() == "primitiveCodec" || object.Name() == "ScalarCodec")
				if adapter && !(name == "New" && shell) || strings.Contains(strings.ToLower(name), "reinterpretcodec") {
					bad = append(bad, body.name+": generic representation adapter")
				}
				unrestricted := path == ormcorePath && (name == "ScalarCodec" || name == "ResultCodec") || path == ormPath && name == "scalarCodec"
				fallback := object.Pkg().Path() == ormPath && (object.Name() == "scalarCodec" || object.Name() == "selectedCodec" || object.Name() == "ColumnOf" || object.Name() == "NullableColumnOf")
				// Raw's approved result fallback uses fixed non-generic dispatch,
				// then the existing scalar shell. No other caller gains this path.
				fallback = fallback || (path == ormcorePath && name == "ScalarCodec" && object.Pkg().Path() == ormcorePath && object.Name() == "ResultCodec") || (path == ormcorePath && name == "ResultCodec" && object.Pkg().Path() == ormPath+"/expr" && object.Name() == "Raw")
				if unrestricted && !fallback {
					bad = append(bad, body.name+": unrestricted codec selector")
				}
				instance, instantiated := info.Instances[id]
				var arguments [][]int
				if instantiated {
					for i := 0; i < instance.TypeArgs.Len(); i++ {
						arguments = append(arguments, codecArgumentParameters(instance.TypeArgs.At(i), parameters))
					}
				}
				if path == codecPath && families[name] {
					affected := map[int]bool{}
					for _, indexes := range arguments {
						for _, i := range indexes {
							affected[i] = true
						}
					}
					// Fixed concrete codecs constructed inside a generic selector are also
					// instantiated in every caller. Fixed dispatch belongs in non-generic code.
					if len(affected) == 0 {
						for i := range parameters {
							affected[i] = true
						}
					}
					for i := range affected {
						body.families[i][name] = true
					}
				}
				if instantiated {
					body.edges = append(body.edges, codecEdge{codecFunctionName(target), arguments})
				}
				return true
			})
		}
	}
	return bad
}

func codecGraphViolations(bodies map[string]*codecBody) []string {
	for changed := true; changed; {
		changed = false
		for _, body := range bodies {
			for _, edge := range body.edges {
				target := bodies[edge.target]
				if target == nil {
					continue
				}
				for argument, indexes := range edge.arguments {
					if argument >= len(target.families) {
						continue
					}
					for _, i := range indexes {
						for family := range target.families[argument] {
							if !body.families[i][family] {
								body.families[i][family] = true
								changed = true
							}
						}
					}
				}
			}
		}
	}
	var bad []string
	for _, body := range bodies {
		for i, families := range body.families {
			if len(families) > 1 {
				var names []string
				for name := range families {
					names = append(names, name)
				}
				sort.Strings(names)
				bad = append(bad, fmt.Sprintf("%s[%s]: multiple codec families: %s", body.name, body.parameters[i].Obj().Name(), strings.Join(names, ", ")))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

func TestCodecInstantiationBound(t *testing.T) {
	bodies := map[string]*codecBody{}
	for _, dir := range []string{".", "./expr", "./rows", "../../internal/ormcore"} {
		_, files, info := surfacePackageSources(t, dir, "")
		for _, bad := range codecBodyViolations(files, info, bodies) {
			t.Error(bad)
		}
	}
	for _, bad := range codecGraphViolations(bodies) {
		t.Error(bad)
	}
}

func TestCodecInstantiationGuardRejectsBypasses(t *testing.T) {
	_, files, info := surfacePackageSources(t, ".", `package orm
 import c "github.com/weiloon1234/Foundry-Go/database/codec"
 import core "github.com/weiloon1234/Foundry-Go/internal/ormcore"
 import "database/sql/driver"
 func guardSigned[V any]() c.Codec[V] { return any(c.Signed[int64]()).(c.Codec[V]) }
 func guardUnsigned[V any]() c.Codec[V] { return any(c.Unsigned[uint64]()).(c.Codec[V]) }
 func guardHop[V any]() { _ = guardSigned[V](); _ = guardUnsigned[V]() }
 func guardValues[V any]() { a,b := guardSigned[V],guardUnsigned[V]; _,_ = a,b }
 func guardConstructorValues[V any]() { a,b:=c.Signed[int64],c.Unsigned[uint64]; _,_=a,b }
 func guardAdapter[V any]() { f:=c.Adapt[V,int64]; _=f }
 func guardInline[V any]() { _=c.New(func(V)(driver.Value,error){return nil,nil},func(any)(V,error){return *new(V),nil}) }
 func guardRenamed[V any]() { _=core.ScalarCodec[V]() }
 func guardRawResult[V any]() { _=core.ResultCodec[V]() }
 func guardSelectorValue[V any]() { f:=core.ScalarCodec[V]; _=f }
 type guardReceiver[V any] struct{}
 func (guardReceiver[V]) hidden() { _=c.Adapt[V,int64] }
 func guardSeparate[V ~int64,W ~uint64]() { _=c.Signed[V](); _=c.Unsigned[W]() }
 `)
	bodies := map[string]*codecBody{}
	violations := codecBodyViolations(files, info, bodies)
	violations = append(violations, codecGraphViolations(bodies)...)
	for _, name := range []string{"guardHop", "guardValues", "guardConstructorValues", "guardAdapter", "guardInline", "guardRenamed", "guardRawResult", "guardSelectorValue", "guardReceiver"} {
		found := false
		for _, violation := range violations {
			if strings.Contains(violation, name) {
				found = true
			}
		}
		if !found {
			t.Errorf("missed %s bypass: %v", name, violations)
		}
	}
	for _, violation := range violations {
		if strings.Contains(violation, "guardSeparate") {
			t.Error("independent value types must not share a family budget:", violation)
		}
	}
}
