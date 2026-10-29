package generate

import (
	"fmt"
	"strings"
)

// scaffoldModelSlots reuses the extension kind and naming rules without depending
// on generated model declarations. Metadata contracts alone still need generation.
func scaffoldModelSlots(o ScaffoldOptions) []extensionSlot {
	var result []extensionSlot
	for _, group := range []struct {
		names []string
		kind  string
	}{
		{o.Translated, textSlot}, {o.Attachment, oneSlot}, {o.Attachments, manySlot}, {o.Metadata, valueSlot},
	} {
		for _, name := range group.names {
			result = append(result, extensionSlot{name: name, stored: snake(name), kind: group.kind})
		}
	}
	return result
}

func renderORMModel(pkg string, o ScaffoldOptions) (string, error) {
	imports, err := developerScaffoldImports(o)
	if err != nil {
		return "", err
	}
	var s strings.Builder
	fmt.Fprintf(&s, "package %s\n\nimport (\n", pkg)
	for _, path := range imports {
		fmt.Fprintf(&s, "%q\n", path)
	}
	s.WriteString(")\n\n")
	name := o.Name
	names, err := allocateScaffoldModelNames(o)
	if err != nil {
		return "", err
	}
	table := names.table
	key := "model.ID[" + name + "]"
	slots := scaffoldModelSlots(o)
	valueType := func(slot extensionSlot) string {
		switch slot.kind {
		case textSlot:
			return "translations.Text"
		case oneSlot, manySlot:
			return "attachments." + slot.kind + "[" + name + "]"
		default:
			return "metadata.Value[" + names.metadata[slot.name] + "]"
		}
	}
	fmt.Fprintf(&s, "// %s is a persisted model. Add fields and their typed handles below.\n// Example: %s{}.\ntype %s struct {\n// ID is the concrete owner key. Example: record.ID.\nID %s `foundry:\"column=id\"`\n", name, name, name, key)
	for _, slot := range slots {
		fmt.Fprintf(&s, "// %s holds %s. Example: record.%s.IsLoaded().\n%s %s\n", slot.name, extensionSlotDescription(slot), slot.name, slot.name, valueType(slot))
	}
	s.WriteString("}\n\n")
	fmt.Fprintf(&s, "// %s holds the model's typed fields. Example: %s.Query().\ntype %s struct {\n// Table owns the primary key and operations. Example: orm.KeyEq(%s.Key(), id).\norm.Table[%s,%s]\n", names.tableType, table, names.tableType, table, name, key)
	// Slot handles use a suffix so every previously accepted model field name
	// (including Table, Key and Query) remains usable beside embedded operations.
	for _, slot := range slots {
		fmt.Fprintf(&s, "// %sSlot binds the external value. Example: extensions.BindSlot(%s.Key(),%s.%sSlot).\n%sSlot orm.Slot[%s,%s]\n", slot.name, table, table, slot.name, slot.name, name, valueType(slot))
	}
	s.WriteString("}\n\n")
	fmt.Fprintf(&s, "// %s declares the persisted table. Example: orm.Find(ctx, db, %s.Table, id).\nvar %s = orm.Define(%q,func(b orm.Builder[%s],m *%s)%s {\nreturn %s{Table:orm.TableOf(b,&m.ID),\n", table, table, table, o.Table, name, name, names.tableType, names.tableType)
	for _, slot := range slots {
		fmt.Fprintf(&s, "%sSlot:orm.SlotOf(b,&m.%s),\n", slot.name, slot.name)
	}
	s.WriteString("}\n})\n\n")
	fmt.Fprintf(&s, "// %s supplies this declaration to the package's Tables helper.\n// Example: orm.Check(%s()...).\nfunc %s() []orm.CheckedTable { return []orm.CheckedTable{%s} }\n", names.tables, names.tables, names.tables, table)
	s.WriteString("\n// Declare relations after the tables they reference to avoid initialization cycles.\n")
	for _, slot := range slots {
		if slot.kind == valueSlot {
			fmt.Fprintf(&s, "\n// %s is %s.%s's stored metadata. Add JSON-tagged fields; removing\n// or renaming one requires a metadata version increment. Example: %s{}.\n//foundry:dto\ntype %s struct{}\n", names.metadata[slot.name], name, slot.name, names.metadata[slot.name], names.metadata[slot.name])
		}
	}
	if len(slots) == 0 {
		return s.String(), nil
	}
	if o.Disk != "" {
		fmt.Fprintf(&s, "\n// %s names the attachment disk; configure it in storage settings.\n// Example: %s.ID().\nvar %s=storage.DefineDisk(%q)\n", names.disk, names.disk, names.disk, o.Disk)
	}
	fmt.Fprintf(&s, "\n// %s pins the persisted owner identity. Keep it stable when renaming tables.\n// Example: %s.Reference(id).\nvar %s=extensions.DefineORMOwner(%q,orm.IdentityOf(%s.Table,%s.Key()),extensions.OwnerOptions{StorageModel:%q})\n", names.owner, names.owner, names.owner, o.Table, table, table, o.Table)
	fmt.Fprintf(&s, "\n// %s groups typed external-store handles. Example: %s.From(runtime).\ntype %s struct {\n", names.extensionType, names.extensions, names.extensionType)
	for _, slot := range slots {
		typ := "translations.TextHandle[" + name + "," + key + "]"
		if slot.kind == oneSlot || slot.kind == manySlot {
			typ = "attachments." + slot.kind + "Handle[" + name + "," + key + "]"
		}
		if slot.kind == valueSlot {
			typ = "metadata.ValueHandle[" + name + "," + key + "," + names.metadata[slot.name] + "]"
		}
		fmt.Fprintf(&s, "// %s configures %s. Example: %s.%s.Registration().\n%s %s\n", slot.name, extensionSlotDescription(slot), names.extensions, slot.name, slot.name, typ)
	}
	s.WriteString("}\n\n")
	fmt.Fprintf(&s, "// %s declares policies independently of table initialization.\n// Example: %s.From(runtime).\nvar %s=%s{\n", names.extensions, names.extensions, names.extensions, names.extensionType)
	for _, slot := range slots {
		binding := fmt.Sprintf("extensions.BindSlot(%s.Key(),%s.%sSlot)", table, table, slot.name)
		switch slot.kind {
		case textSlot:
			fmt.Fprintf(&s, "%s:translations.TextOf(%s,%q,translations.Options{},%s),\n", slot.name, names.owner, slot.stored, binding)
		case oneSlot, manySlot:
			s.WriteString("// List accepted media types (or an Image plan); assembly rejects an empty policy.\n")
			fmt.Fprintf(&s, "%s:attachments.%sOf(%s,%q,attachments.Policy{Disk:%s,Accepted:[]storage.MediaType{}},%s),\n", slot.name, slot.kind, names.owner, slot.stored, names.disk, binding)
		case valueSlot:
			codec, _ := jsonDescriptorNames(names.metadata[slot.name])
			fmt.Fprintf(&s, "%s:metadata.ValueOf(%s,%q,metadata.Spec[%s]{},%s(),%s),\n", slot.name, names.owner, slot.stored, names.metadata[slot.name], codec, binding)
		}
	}
	s.WriteString("}\n\n")
	fmt.Fprintf(&s, "// From borrows runtime managers once. Example: %s.From(runtime).\nfunc (d %s) From(runtime slots.Runtime) %s {\n", names.extensions, names.extensionType, names.extensionType)
	for _, slot := range slots {
		manager := "Attachments"
		if slot.kind == textSlot {
			manager = "Translations"
		}
		if slot.kind == valueSlot {
			manager = "Metadata"
		}
		fmt.Fprintf(&s, "d.%s=d.%s.From(runtime.%s)\n", slot.name, slot.name, manager)
	}
	s.WriteString("return d\n}\n\n")
	fmt.Fprintf(&s, "// All returns typed eager loaders. Example: %s.Query().With(bound.All()...).\nfunc (d %s) All() []orm.Relation[%s] {return []orm.Relation[%s]{\n", table, names.extensionType, name, name)
	for _, slot := range slots {
		fmt.Fprintf(&s, "d.%s.Relation(%s.%sSlot),\n", slot.name, table, slot.name)
	}
	s.WriteString("}}\n\n")
	fmt.Fprintf(&s, "// %s includes ordinary deletion cleanup.\n// Example: builder.Models(%s()).\nfunc %s() slots.Declaration {return slots.DeclareORM(%s,slots.Parts{\n", names.declaration, names.declaration, names.declaration, names.owner)
	for _, group := range []struct {
		field, pkg string
		kinds      []string
	}{{"Translations", "translations", []string{textSlot}}, {"Attachments", "attachments", []string{oneSlot, manySlot}}, {"Metadata", "metadata", []string{valueSlot}}} {
		var entries []string
		for _, slot := range slots {
			for _, kind := range group.kinds {
				if slot.kind == kind {
					entries = append(entries, names.extensions+"."+slot.name+".Registration()")
				}
			}
		}
		if len(entries) > 0 {
			fmt.Fprintf(&s, "%s:[]%s.Registration{%s},\n", group.field, group.pkg, strings.Join(entries, ","))
		}
	}
	s.WriteString("Slots:[]extensions.SlotDescription{\n")
	for _, slot := range slots {
		fmt.Fprintf(&s, "%s.%s.Describe(),\n", names.extensions, slot.name)
	}
	s.WriteString("},\n})}\n")
	return s.String(), nil
}
