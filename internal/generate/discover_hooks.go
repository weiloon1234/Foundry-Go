package generate

import (
	"go/ast"
	"go/token"
)

// The factory explicitly references generated hook types, so its complete
// signature/body is checked in the generated overlay rather than the first
// declaration pass. Requiring a named function avoids mutable global registries.
func discoverHooks(p *packageInput, m model) error {
	if err := discoverHookFactory(p, m, "hooks", m.hooks, "Hooks"); err != nil {
		return err
	}
	return discoverHookFactory(p, m, "retrieval", m.retrieval, "RetrievalHooks")
}

func discoverHookFactory(p *packageInput, m model, option, name, suffix string) error {
	if name == "" {
		return nil
	}
	if token.IsIdentifier(name) && name != "_" {
		for _, file := range p.files {
			for _, declaration := range file.syntax.Decls {
				f, ok := declaration.(*ast.FuncDecl)
				if ok && f.Recv == nil && f.Name.Name == name && f.Type.Params.NumFields() == 0 && f.Type.TypeParams.NumFields() == 0 && f.Type.Results.NumFields() == 1 {
					return nil
				}
			}
		}
	}
	return p.diagnostic(m.typ.Obj().Pos(), "model "+option+" requires a package function with no arguments returning "+m.name+suffix)
}
