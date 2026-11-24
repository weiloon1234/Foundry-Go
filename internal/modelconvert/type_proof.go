package modelconvert

import (
	"fmt"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

type tableContract struct {
	VariableType string
	Members      []tableMember
	Methods      []tableMethod
}
type tableMember struct {
	Name, Type, KeyCapability string
	Embedded                  bool
}
type tableMethod struct{ Name, Receiver, Signature string }

func tableContracts(pkg *generate.AnalysisPackage, names []string) (map[string]tableContract, error) {
	result := map[string]tableContract{}
	for _, name := range names {
		object, ok := pkg.Types.Scope().Lookup(name).(*types.Var)
		if !ok {
			return nil, reject("table variable missing: " + name)
		}
		t := types.Unalias(object.Type())
		contract := tableContract{VariableType: contractType(t)}
		if pointer, ok := t.(*types.Pointer); ok {
			t = types.Unalias(pointer.Elem())
		}
		named, ok := t.(*types.Named)
		if !ok {
			return nil, reject("table variable has no named type: " + name)
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			return nil, reject("table variable is no longer a struct: " + name)
		}
		for i := 0; i < st.NumFields(); i++ {
			field := st.Field(i)
			contract.Members = append(contract.Members, tableMember{Name: field.Name(), Type: contractType(field.Type()), Embedded: field.Embedded(), KeyCapability: keyCapability(field.Type())})
		}
		sort.Slice(contract.Members, func(i, j int) bool { return contract.Members[i].Name < contract.Members[j].Name })
		for i := 0; i < named.NumMethods(); i++ {
			method := named.Method(i)
			signature := method.Type().(*types.Signature)
			contract.Methods = append(contract.Methods, tableMethod{Name: method.Name(), Receiver: contractType(signature.Recv().Type()), Signature: contractType(signature)})
		}
		sort.Slice(contract.Methods, func(i, j int) bool { return contract.Methods[i].Name < contract.Methods[j].Name })
		result[name] = contract
	}
	return result, nil
}
func contractType(t types.Type) string {
	return types.TypeString(types.Unalias(t), func(p *types.Package) string { return p.Path() })
}
func keyCapability(t types.Type) string {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != ormPath || named.Obj().Name() != "ColumnHandle" || named.TypeArgs().Len() != 6 {
		return ""
	}
	role, ok := types.Unalias(named.TypeArgs().At(5)).(*types.Named)
	if !ok {
		return ""
	}
	switch role.Obj().Name() {
	case "LookupKeyRole":
		return "lookup"
	case "PrimaryRole":
		return "primary"
	case "InputKeyRole":
		return "input"
	}
	return ""
}
func compareTableContracts(before, after map[string]tableContract) []string {
	var differences []string
	add := func(path, property, left, right string) {
		differences = append(differences, fmt.Sprintf("%s differs: hand-written %q, generated %q (%s)", property, left, right, path))
	}
	names := make([]string, 0, len(before))
	for name := range before {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a := before[name]
		b, ok := after[name]
		if !ok {
			add(name+".missing", "table variable "+name, a.VariableType, "absent")
			continue
		}
		if a.VariableType != b.VariableType {
			add(name+".variable_type", "table variable type", a.VariableType, b.VariableType)
		}
		if len(a.Members) != len(b.Members) {
			add(name+".members.length", "table member count", strconv.Itoa(len(a.Members)), strconv.Itoa(len(b.Members)))
		}
		for i := 0; i < min(len(a.Members), len(b.Members)); i++ {
			x, y := a.Members[i], b.Members[i]
			prefix := fmt.Sprintf("%s.members[%d]", name, i)
			if x.Name != y.Name {
				add(prefix+".name", x.Name+": member name", x.Name, y.Name)
			}
			if x.Type != y.Type {
				add(prefix+".type", x.Name+": member type", x.Type, y.Type)
			}
			if x.Embedded != y.Embedded {
				add(prefix+".embedded", x.Name+": embedding", strconv.FormatBool(x.Embedded), strconv.FormatBool(y.Embedded))
			}
			if x.KeyCapability != y.KeyCapability {
				add(prefix+".key_capability", x.Name+": key capability", x.KeyCapability, y.KeyCapability)
			}
		}
		for _, method := range a.Methods {
			if !slices.Contains(b.Methods, method) {
				generated := "absent"
				for _, actual := range b.Methods {
					if actual.Name == method.Name {
						generated = actual.Receiver + " " + actual.Signature
						break
					}
				}
				add(name+".methods."+method.Name, "table method "+method.Name, method.Receiver+" "+method.Signature, generated)
			}
		}
		for _, method := range b.Methods {
			found := false
			for _, old := range a.Methods {
				if old.Name == method.Name {
					found = true
					break
				}
			}
			if !found {
				add(name+".methods."+method.Name, "table method "+method.Name, "absent", method.Receiver+" "+method.Signature)
			}
		}
	}
	return differences
}
func checkTableTypes(before, after *generate.AnalysisPackage, models []Model) error {
	var failures []string
	for _, model := range models {
		names := []string{model.Variable}
		a, err := tableContracts(before, names)
		if err != nil {
			return err
		}
		b, err := tableContracts(after, names)
		if err != nil {
			return err
		}
		position := before.Fset.Position(before.Types.Scope().Lookup(model.Name).Pos())
		for _, difference := range compareTableContracts(a, b) {
			failures = append(failures, position.String()+": model "+model.Name+": table type contract changed: "+difference)
		}
	}
	if len(failures) > 0 {
		return reject(strings.Join(failures, "\n"))
	}
	return nil
}
