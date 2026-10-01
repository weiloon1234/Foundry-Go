package generate

import (
	"fmt"
	"strings"
)

// extensionsFunction lists one package's generated extension declarations.
const extensionsFunction = "FoundryExtensions"

// extensionStorage names the framework table each slot kind is stored in.
var extensionStorage = map[string]string{
	textSlot:  "foundry_model_translations",
	oneSlot:   "foundry_attachments",
	manySlot:  "foundry_attachments",
	valueSlot: "foundry_model_metadata",
}

// extensionSlotDescription is the shared one-line summary for generated
// documentation and handwritten field notices.
func extensionSlotDescription(slot extensionSlot) string {
	switch slot.kind {
	case textSlot:
		return fmt.Sprintf("translated text stored in %s as field %q, one row per locale", extensionStorage[slot.kind], slot.stored)
	case oneSlot:
		return fmt.Sprintf("the single file of attachment collection %q, recorded in %s with its bytes on the policy's disk", slot.stored, extensionStorage[slot.kind])
	case manySlot:
		return fmt.Sprintf("the ordered files of attachment collection %q, recorded in %s with their bytes on the policy's disk", slot.stored, extensionStorage[slot.kind])
	}
	return fmt.Sprintf("the typed metadata value %q stored in %s, with no model column", slot.stored, extensionStorage[slot.kind])
}

func (e *emitter) emitModelExtensions(m model, primary field) {
	if len(m.extensions) == 0 {
		return
	}
	query, extensions := e.use(framework+"/database/query"), e.use(framework+"/extensions")
	slots, foundation := e.use(framework+"/extensions/slots"), e.use(framework+"/foundation")
	context, database := e.use("context"), e.use(framework+"/database")
	key := e.typeName(primary.typ)
	owner := fmt.Sprintf("%s.Owner[%s,%s]", extensions, m.name, key)
	// Import only the extension packages whose slot kinds the model declares.
	kinds := map[string]string{}
	for _, slot := range m.extensions {
		switch slot.kind {
		case textSlot:
			kinds[slot.kind] = e.use(framework + "/translations")
		case oneSlot, manySlot:
			kinds[slot.kind] = e.use(framework + "/attachments")
		case valueSlot:
			kinds[slot.kind] = e.use(framework + "/metadata")
		}
	}
	policyType := func(slot extensionSlot) string {
		switch slot.kind {
		case textSlot:
			return kinds[textSlot] + ".Options"
		case oneSlot, manySlot:
			return kinds[slot.kind] + ".Policy"
		}
		return fmt.Sprintf("%s.Spec[%s]", kinds[valueSlot], e.typeName(slot.value))
	}
	slotType := func(slot extensionSlot) string {
		switch slot.kind {
		case textSlot:
			return kinds[textSlot] + ".Text"
		case oneSlot, manySlot:
			return fmt.Sprintf("%s.%s[%s]", kinds[slot.kind], slot.kind, m.name)
		}
		return fmt.Sprintf("%s.Value[%s]", kinds[valueSlot], e.typeName(slot.value))
	}
	descriptorType := func(slot extensionSlot) string {
		switch slot.kind {
		case textSlot:
			return fmt.Sprintf("%s.TextSlot[%s,%s]", kinds[textSlot], m.name, key)
		case oneSlot, manySlot:
			return fmt.Sprintf("%s.%sSlot[%s,%s]", kinds[slot.kind], slot.kind, m.name, key)
		}
		return fmt.Sprintf("%s.ValueSlot[%s,%s,%s]", kinds[valueSlot], m.name, key, e.typeName(slot.value))
	}

	e.line("// %sExtensionSet declares the policy of every extension slot of %s.", m.name, m.name)
	e.line("// %s.%s returns it; a zero entry selects that slot kind's defaults, except that attachment slots require a Disk.", m.name, extensionSetMethod)
	e.line("type %sExtensionSet struct{", m.name)
	for _, slot := range m.extensions {
		e.line("// %s configures %s.", slot.name, extensionSlotDescription(slot))
		e.line("%s %s", slot.name, policyType(slot))
	}
	e.line("}")

	e.line("// %sExtensionSlots holds typed descriptors for the extension slots of %s.", m.name, m.name)
	e.line("// Bind managers once in a constructor with From(runtime); an unbound descriptor reports fault.Missing when used.")
	e.line("type %sExtensionSlots struct{", m.name)
	for _, slot := range m.extensions {
		e.line("// %s describes %s.%s: %s.", slot.name, m.name, slot.name, extensionSlotDescription(slot))
		e.line("%s %s", slot.name, descriptorType(slot))
	}
	e.line("}")

	e.line("var foundry%sExtensionOwner %s.Memo[%s]", m.name, query, owner)
	e.line("// %sExtensionOwner is the extension owner of %s, with owner name and storage model %q.", m.name, m.name, m.extensionOwner)
	e.line("// Explicit translation, attachment and metadata APIs reuse it together with %sExtensions(). Keep its name when renaming the table.", m.name)
	e.line("func %sExtensionOwner()%s{return foundry%sExtensionOwner.Get(func()%s{return %s.DefineOwnerWith(%q,%s.IdentityOf(%s().Query,%sFields().%s),%s.OwnerOptions{StorageModel:%q})})}", m.name, owner, m.name, owner, extensions, m.extensionOwner, query, m.query, m.name, primary.name, extensions, m.extensionOwner)

	policy, ownerVar := e.localName("policy"), e.localName("owner")
	e.line("var foundry%sExtensions %s.Memo[%sExtensionSlots]", m.name, query, m.name)
	e.line("func foundry%sBuildExtensions()%sExtensionSlots{", m.name, m.name)
	if m.extensionPolicy {
		e.line("%s:=(%s{}).%s()", policy, m.name, extensionSetMethod)
	} else {
		e.line("var %s %sExtensionSet", policy, m.name)
	}
	e.line("%s:=%sExtensionOwner()", ownerVar, m.name)
	e.line("return %sExtensionSlots{", m.name)
	for _, slot := range m.extensions {
		binding := fmt.Sprintf("%s.SlotBinding[%s,%s,%s]{Field:%q,Reference:%s.FoundryReference,Get:func(m %s)%s{return m.%s},Set:func(m %s,slot %s)%s{m.%s=slot;return m}}", extensions, m.name, key, slotType(slot), slot.name, m.name, m.name, slotType(slot), slot.name, m.name, slotType(slot), m.name, slot.name)
		switch slot.kind {
		case textSlot:
			e.line("%s:%s.DefineText(%s,%q,%s.%s,%s),", slot.name, kinds[textSlot], ownerVar, slot.stored, policy, slot.name, binding)
		case oneSlot, manySlot:
			e.line("%s:%s.Define%s(%s,%q,%s.%s,%s),", slot.name, kinds[slot.kind], slot.kind, ownerVar, slot.stored, policy, slot.name, binding)
		case valueSlot:
			e.line("%s:%s.DefineValue(%s,%q,%s.%s,%s,%s),", slot.name, kinds[valueSlot], ownerVar, slot.stored, policy, slot.name, e.metadataContract(slot), binding)
		}
	}
	e.line("}}")
	e.line("// %sExtensions returns unbound descriptors for the extension slots of %s, built once per process.", m.name, m.name)
	e.line("// %s runs once and must not call this accessor. Bind the descriptors with From before loading or writing.", extensionSetMethod)
	e.line("func %sExtensions()%sExtensionSlots{return foundry%sExtensions.Get(foundry%sBuildExtensions)}", m.name, m.name, m.name, m.name)

	runtime, descriptors := e.localName("runtime"), e.localName("s")
	e.line("// From returns descriptors that borrow the runtime's managers. Bind once in a constructor, for example")
	e.line("// %sExtensions().From(services.ModelExtensions()); descriptors never look up services per call.", m.name)
	e.line("func(%s %sExtensionSlots)From(%s %s.Runtime)%sExtensionSlots{", descriptors, m.name, runtime, slots, m.name)
	for _, slot := range m.extensions {
		manager := map[string]string{textSlot: "Translations", oneSlot: "Attachments", manySlot: "Attachments", valueSlot: "Metadata"}[slot.kind]
		e.line("%s.%s=%s.%s.From(%s.%s)", descriptors, slot.name, descriptors, slot.name, runtime, manager)
	}
	e.line("return %s}", descriptors)
	var relations []string
	for _, slot := range m.extensions {
		relations = append(relations, descriptors+"."+slot.name)
	}
	e.line("// All returns every slot descriptor as an eager-loading relation, for example")
	e.line("// Query%s().With(x.All()...) on an edit screen. Unbound descriptors fail validation before SQL.", strings.TrimPrefix(m.query, "Query"))
	e.line("func(%s %sExtensionSlots)All()[]%s.Relation[%s]{return []%s.Relation[%s]{%s}}", descriptors, m.name, query, m.name, query, m.name, strings.Join(relations, ","))

	parts, registrar, pool, resolve := e.localName("parts"), e.localName("registrar"), e.localName("pool"), e.localName("resolve")
	resolver, name, errName, cleanup := e.localName("resolver"), e.localName("name"), e.localName("err"), e.localName("cleanup")
	hooks, ctx, tx, changes := e.localName("hooks"), e.localName("ctx"), e.localName("tx"), e.localName("changes")
	registrations, packages := map[string][]string{}, map[string]string{}
	for _, slot := range m.extensions {
		group := map[string]string{textSlot: "Translations", oneSlot: "Attachments", manySlot: "Attachments", valueSlot: "Metadata"}[slot.kind]
		registrations[group] = append(registrations[group], descriptors+"."+slot.name+".Registration()")
		packages[group] = kinds[slot.kind]
	}
	e.line("// %sExtensionDeclaration registers the extension owner and slots of %s, plus a deletion observer", m.name, m.name)
	e.line("// that removes its extension data when a model is hard-deleted through any connection. Register it with application Builder.Models.")
	e.line("func %sExtensionDeclaration()%s.Declaration{", m.name, slots)
	e.line("%s:=%sExtensions()", descriptors, m.name)
	e.line("%s:=%s.Parts{", parts, slots)
	for _, group := range []string{"Translations", "Attachments", "Metadata"} {
		if len(registrations[group]) > 0 {
			e.line("%s:[]%s.Registration{%s},", group, packages[group], strings.Join(registrations[group], ","))
		}
	}
	descriptions := make([]string, len(m.extensions))
	for i, slot := range m.extensions {
		descriptions[i] = descriptors + "." + slot.name + ".Describe()"
	}
	e.line("Slots:[]%s.SlotDescription{%s},", extensions, strings.Join(descriptions, ","))
	e.line("}")
	e.line("return %s.Declare(%sExtensionOwner().Registration(),%s,func(%s *%s.Registrar,%s %s.Key[*%s.DB],%s %s.ResolveRuntime)error{", slots, m.name, parts, registrar, foundation, pool, foundation, database, resolve, slots)
	e.line("%s,%s:=%s.ObserverName[%s]();if %s!=nil{return %s}", name, errName, slots, m.name, errName, errName)
	// A deletion observer leaves creates, updates and set-based updates on
	// their unhooked paths on every connection it is registered on.
	lifecycle := e.use(framework + "/database/lifecycle")
	e.line("return Register%sObserver(%s,%s,%s.NewDeletionObserver[%s,%sHooks](%s),func(%s %s.Resolver)(func()%sHooks,error){", m.name, registrar, pool, lifecycle, m.name, m.name, name, resolver, foundation, m.name)
	e.line("%s,%s:=%s(%s);if %s!=nil{return nil,%s}", runtime, errName, resolve, resolver, errName, errName)
	e.line("%s,%s:=%s.NewCleanup(%s,%sExtensionOwner(),%s,%s.FoundryReference);if %s!=nil{return nil,%s}", cleanup, errName, slots, runtime, m.name, parts, m.name, errName, errName)
	e.line("%s:=%sHooks{Deleted:func(%s %s.Context,%s *%s.Tx,%s %sChanges)error{return %s.Deleted(%s,%s,%s.Before(),%s.Operation())}}", hooks, m.name, ctx, context, tx, database, changes, m.name, cleanup, ctx, tx, changes, changes)
	e.line("return func()%sHooks{return %s},nil})})}", m.name, hooks)
}

// metadataContract emits the inferred JSON descriptor, or a zero descriptor
// that DefineExtensions must replace.
func (e *emitter) metadataContract(slot extensionSlot) string {
	// Import contract only for expressions that name it; a same-package DTO
	// descriptor call must not leave an unused import.
	contract := func() string { return e.useNamed(framework+"/contract", "foundrycontract") }
	value := e.typeName(slot.value)
	switch slot.contract.kind {
	case "dto":
		if slot.contract.pkg != nil {
			return e.useNamed(slot.contract.pkg.Path(), slot.contract.pkg.Name()) + "." + slot.contract.function + "()"
		}
		return slot.contract.function + "()"
	case "scalar":
		return fmt.Sprintf("%s.%s[%s]()", contract(), slot.contract.function, value)
	case "dynamic":
		return contract() + ".DynamicJSON()"
	case "custom":
		return fmt.Sprintf("(*new(%s)).JSONContract()", value)
	}
	return fmt.Sprintf("%s.JSON[%s]{}", contract(), value)
}

// emitPackageExtensions lists every slot-owning model's declaration, so one
// assembly call registers a whole model package.
func emitPackageExtensions(p *packageInput, models []model) ([]byte, error) {
	e := newEmitter(p)
	slots := e.use(framework + "/extensions/slots")
	var declarations []string
	for _, m := range models {
		if len(m.extensions) > 0 {
			declarations = append(declarations, m.name+"ExtensionDeclaration()")
		}
	}
	e.line("// %s lists this package's model extension declarations in model name order.", extensionsFunction)
	e.line("// Register them once with application Builder.Models(%s()...).", extensionsFunction)
	e.line("func %s()[]%s.Declaration{return []%s.Declaration{%s}}", extensionsFunction, slots, slots, strings.Join(declarations, ","))
	for _, m := range models {
		if len(m.extensions) > 0 {
			return e.finish(m.position.Filename)
		}
	}
	return nil, nil
}
