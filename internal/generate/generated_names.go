package generate

import (
	"fmt"
	"go/ast"
	"go/token"
)

// checkGeneratedNames reports a generated package-level symbol or method that
// a handwritten declaration already owns. Both source positions are named, so
// the collision is actionable before the overlay reports a redeclaration.
// Scaffold overlays pass no origins: their new file is handwritten.
func (p *packageInput) checkGeneratedNames(outputs map[string]*ast.File, origins map[string]token.Position) error {
	if origins == nil {
		return nil
	}
	generated := make(map[string]string)
	for _, name := range sortedNames(outputs) {
		origin := origins[name]
		collision := func(symbol string, handwritten token.Pos) error {
			return fmt.Errorf("%s: %s is declared by handwritten code, but the Foundry declaration at %s generates it in %s; rename the handwritten declaration or the Foundry declaration", p.fset.Position(handwritten), symbol, origin, p.displayName(name))
		}
		// Two declarations can derive the same helper name, for example a path
		// and an enum whose names differ only by a generated suffix.
		duplicate := func(symbol string) error {
			previous, exists := generated[symbol]
			if !exists {
				generated[symbol] = name
				return nil
			}
			return fmt.Errorf("%s: generated symbol %s in %s is also generated in %s by the declaration at %s; rename one declaration", origin, symbol, p.displayName(name), p.displayName(previous), origins[previous])
		}
		for _, decl := range outputs[name].Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					if position, exists := p.definedAt[decl.Name.Name]; exists {
						return collision(decl.Name.Name, position)
					}
					if err := duplicate(decl.Name.Name); err != nil {
						return err
					}
					continue
				}
				if len(decl.Recv.List) != 1 {
					continue
				}
				owner := receiverTypeName(decl.Recv.List[0].Type)
				if position, exists := p.methods[owner][decl.Name.Name]; exists {
					return collision(owner+"."+decl.Name.Name, position)
				}
				if err := duplicate(owner + "." + decl.Name.Name); err != nil {
					return err
				}
			case *ast.GenDecl:
				if decl.Tok == token.IMPORT {
					continue
				}
				for _, spec := range decl.Specs {
					var names []*ast.Ident
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						names = []*ast.Ident{spec.Name}
					case *ast.ValueSpec:
						names = spec.Names
					}
					for _, ident := range names {
						if ident.Name == "_" {
							continue
						}
						if position, exists := p.definedAt[ident.Name]; exists {
							return collision(ident.Name, position)
						}
						if err := duplicate(ident.Name); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
