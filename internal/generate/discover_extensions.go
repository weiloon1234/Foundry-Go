package generate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// extensionSetMethod is the optional handwritten model method returning the
// generated policy set. Its result type is generated, so discovery validates
// its declaration syntactically and the complete overlay checks its body.
const extensionSetMethod = "DefineExtensions"

// Extension slot kinds, named after their runtime slot types.
const (
	textSlot  = "Text"
	oneSlot   = "One"
	manySlot  = "Many"
	valueSlot = "Value"
)

// Generated descriptor-set methods; slot fields cannot use these names.
var reservedExtensionSlotNames = map[string]bool{"From": true, "All": true}

// extensionSlot is a non-persisted model field backed by a framework
// extension store: translated text, an attachment collection or metadata.
type extensionSlot struct {
	name, stored, kind string
	pinned             bool       // foundry:"name=..." declares the stored name.
	value              types.Type // Value slots only.
	contract           metadataContract
	position           token.Position
	pos                token.Pos
}

// metadataContract is the JSON descriptor generation infers for a Value slot.
// An empty kind infers none; DefineExtensions must then declare one.
type metadataContract struct {
	kind, function string
	pkg            *types.Package // Imported DTO descriptors.
}

// extensionSlotKind classifies a field type as an extension slot.
func extensionSlotKind(typ types.Type) (string, *types.Named) {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok {
		return "", nil
	}
	switch {
	case isNamed(named, framework+"/translations", "Text"):
		return textSlot, named
	case isNamed(named, framework+"/attachments", "One"):
		return oneSlot, named
	case isNamed(named, framework+"/attachments", "Many"):
		return manySlot, named
	case isNamed(named, framework+"/metadata", "Value"):
		return valueSlot, named
	}
	return "", nil
}

func discoverExtensionSlot(p *packageInput, v *types.Var, tag map[string]string, owner *types.Named) (extensionSlot, bool, error) {
	kind, wrapper := extensionSlotKind(v.Type())
	if kind == "" {
		return extensionSlot{}, false, nil
	}
	slot := extensionSlot{name: v.Name(), stored: tag["name"], pinned: tag["name"] != "", kind: kind, position: p.fset.Position(v.Pos()), pos: v.Pos()}
	if tag["column"] != "" || tag["default"] != "" {
		return slot, true, p.diagnostic(v.Pos(), "extension slots cannot declare persistence column/default tags; use foundry:\"name=stored_name\" to pin the stored name")
	}
	if slot.stored == "" {
		slot.stored = snake(v.Name())
	}
	if !identifier.Semantic(slot.stored) {
		return slot, true, p.diagnostic(v.Pos(), "extension slot stored name must be lowercase letters, digits, '_', '.' or '-'")
	}
	if reservedExtensionSlotNames[slot.name] {
		return slot, true, p.diagnostic(v.Pos(), "extension slot "+slot.name+" conflicts with a generated descriptor method; rename the field and pin its stored name with foundry:\"name=...\"")
	}
	switch kind {
	case oneSlot, manySlot:
		if !types.Identical(wrapper.TypeArgs().At(0), owner) {
			return slot, true, p.diagnostic(v.Pos(), "attachment slot type argument must be the enclosing model "+owner.Obj().Name())
		}
	case valueSlot:
		slot.value = wrapper.TypeArgs().At(0)
	}
	return slot, true, nil
}

// finishExtensions validates model-level slot declarations once all fields are
// known: unique stored names, owner identity and the policy method.
func finishExtensions(p *packageInput, spec *ast.TypeSpec, m *model, owner string) error {
	if len(m.extensions) == 0 {
		if owner != "" {
			return p.diagnostic(spec.Pos(), "extension_owner requires extension slot fields")
		}
		if declared, found := p.methods[m.name][extensionSetMethod]; found {
			return p.diagnostic(declared, "model "+extensionSetMethod+" requires extension slot fields")
		}
		return nil
	}
	stored := make(map[string]bool, len(m.extensions))
	for _, slot := range m.extensions {
		if stored[slot.stored] {
			return p.diagnostic(slot.pos, "duplicate extension slot stored name "+slot.stored)
		}
		stored[slot.stored] = true
	}
	m.extensionOwner = owner
	if m.extensionOwner == "" {
		m.extensionOwner = m.table
	}
	if !identifier.Semantic(m.extensionOwner) || !sqlname.Table(m.extensionOwner) {
		return p.diagnostic(spec.Pos(), "extension owner "+m.extensionOwner+" must be a lowercase table-style name; declare extension_owner=name")
	}
	declared, found := p.methods[m.name][extensionSetMethod]
	m.extensionPolicy = found
	if found {
		if !validExtensionSetMethod(p, m.name) {
			return p.diagnostic(declared, "model "+extensionSetMethod+" requires a value receiver and signature func ("+m.name+") "+extensionSetMethod+"() "+m.name+"ExtensionSet")
		}
		return nil
	}
	for _, slot := range m.extensions {
		if slot.kind == oneSlot || slot.kind == manySlot {
			return p.diagnostic(slot.pos, "attachment slot "+slot.name+" requires a policy; declare func ("+m.name+") "+extensionSetMethod+"() "+m.name+"ExtensionSet")
		}
	}
	return nil
}

// validExtensionSetMethod checks the handwritten declaration syntactically;
// its generated result type does not exist during declaration analysis.
func validExtensionSetMethod(p *packageInput, owner string) bool {
	for _, file := range p.files {
		for _, decl := range file.syntax.Decls {
			method, ok := decl.(*ast.FuncDecl)
			if !ok || method.Recv == nil || method.Name.Name != extensionSetMethod || len(method.Recv.List) != 1 || receiverTypeName(method.Recv.List[0].Type) != owner {
				continue
			}
			if _, pointer := method.Recv.List[0].Type.(*ast.StarExpr); pointer || method.Type.TypeParams != nil || method.Type.Params.NumFields() != 0 || method.Type.Results.NumFields() != 1 {
				return false
			}
			result, ok := method.Type.Results.List[0].Type.(*ast.Ident)
			return ok && result.Name == owner+"ExtensionSet"
		}
	}
	return false
}

// inferMetadataContracts runs after discovery, when same-package DTO
// descriptors are known. A slot without an inferable contract needs one in
// DefineExtensions; a model without that method fails generation.
func inferMetadataContracts(p *packageInput, models []model, dtos []dtoDeclaration) error {
	local := make(map[*types.Named]bool, len(dtos))
	for _, declaration := range dtos {
		if declaration.typ.TypeParams().Len() == 0 {
			local[declaration.typ] = true
		}
	}
	for i := range models {
		m := &models[i]
		for j := range m.extensions {
			slot := &m.extensions[j]
			if slot.kind != valueSlot {
				continue
			}
			var problem string
			slot.contract, problem = inferMetadataContract(p, slot.value, local)
			if problem != "" {
				return p.diagnostic(slot.pos, "metadata slot "+slot.name+": "+problem)
			}
			if slot.contract.kind == "" && !m.extensionPolicy {
				return p.diagnostic(slot.pos, "metadata slot "+slot.name+" needs an explicit JSON contract; declare func ("+m.name+") "+extensionSetMethod+"() "+m.name+"ExtensionSet with metadata.Spec{JSON: ...}")
			}
		}
	}
	return nil
}

// inferMetadataContract returns the inferred descriptor, or a problem when
// the type declares a malformed JSONContract. Enums are not inferred: a scalar
// descriptor would decode values outside their membership.
func inferMetadataContract(p *packageInput, typ types.Type, local map[*types.Named]bool) (metadataContract, string) {
	if rawMessage(p, typ) {
		return metadataContract{kind: "dynamic"}, ""
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok {
		if p.enumTypes[named] || hasEnumDescriptor(named) {
			return metadataContract{}, ""
		}
		if named.TypeArgs().Len() == 0 {
			if local[named] {
				return metadataContract{kind: "dto", function: named.Obj().Name() + "JSON"}, ""
			}
			if pkg := named.Obj().Pkg(); pkg != nil && pkg.Path() != p.path && importedDescriptor(pkg, named) {
				return metadataContract{kind: "dto", function: named.Obj().Name() + "JSON", pkg: pkg}, ""
			}
			ok, problem := customDTOContract(named)
			if ok {
				return metadataContract{kind: "custom"}, ""
			}
			if problem != "" {
				return metadataContract{}, problem
			}
		}
	}
	if basic, ok := types.Unalias(typ).Underlying().(*types.Basic); ok {
		switch {
		case basic.Kind() == types.String:
			return metadataContract{kind: "scalar", function: "StringJSON"}, ""
		case basic.Kind() == types.Bool:
			return metadataContract{kind: "scalar", function: "BooleanJSON"}, ""
		case basic.Info()&types.IsInteger != 0 && basic.Kind() != types.Uintptr:
			return metadataContract{kind: "scalar", function: "IntegerJSON"}, ""
		case basic.Info()&types.IsFloat != 0:
			return metadataContract{kind: "scalar", function: "NumberJSON"}, ""
		}
	}
	return metadataContract{}, ""
}

// importedDescriptor recognizes a generated DTO descriptor, func XJSON()
// contract.JSON[X], in an already generated dependency.
func importedDescriptor(pkg *types.Package, named *types.Named) bool {
	function, ok := pkg.Scope().Lookup(named.Obj().Name() + "JSON").(*types.Func)
	if !ok {
		return false
	}
	sig := function.Type().(*types.Signature)
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 || sig.TypeParams().Len() != 0 {
		return false
	}
	result, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
	return ok && isNamed(result, framework+"/contract", "JSON") && result.TypeArgs().Len() == 1 && types.Identical(result.TypeArgs().At(0), named)
}

// rawMessage compares with encoding/json.RawMessage through the package
// importer, because it is an alias of jsontext.Value under JSON v2.
func rawMessage(p *packageInput, typ types.Type) bool {
	pkg, err := p.importer.Import("encoding/json")
	if err != nil {
		return false
	}
	object, ok := pkg.Scope().Lookup("RawMessage").(*types.TypeName)
	return ok && types.Identical(typ, object.Type())
}
