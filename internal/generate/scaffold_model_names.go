package generate

import (
	"fmt"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

type scaffoldModelNames struct {
	table, tableType, tables                            string
	disk, owner, extensionType, extensions, declaration string
	metadata                                            map[string]string
}

// allocateScaffoldModelNames reserves all defaults before assigning suffixes.
// Structural declarations have priority over the table namespace and metadata;
// a fallback must not steal another declaration's otherwise available default.
func allocateScaffoldModelNames(o ScaffoldOptions) (scaffoldModelNames, error) {
	parts := strings.Split(o.Table, ".")
	table := exportedName(parts[len(parts)-1])
	if table == o.Name {
		table += "s"
	}
	// SQL identifiers may begin with underscores that exportedName removes.
	// Preserve the SQL name, but prefix an empty or digit-leading Go namespace.
	if !validScaffoldIdentifier(table) {
		table = "Query" + table
	}
	n := scaffoldModelNames{
		table: table, tableType: o.Name + "Table", tables: o.Name + "Tables",
		disk: o.Name + "Disk", owner: o.Name + "ExtensionOwner",
		extensionType: o.Name + "ExtensionSlots", extensions: o.Name + "Extensions",
		declaration: o.Name + "ExtensionDeclaration", metadata: make(map[string]string),
	}
	type request struct {
		name     *string
		metadata bool
	}
	model := o.Name
	requests := []request{{&model, false}, {&n.tableType, false}, {&n.tables, false}}
	if o.Disk != "" {
		requests = append(requests, request{&n.disk, false})
	}
	if o.hasSlots() {
		for _, name := range []*string{&n.owner, &n.extensionType, &n.extensions, &n.declaration} {
			requests = append(requests, request{name, false})
		}
	}
	requests = append(requests, request{&n.table, false})
	metadataNames := make([]string, len(o.Metadata))
	for i, slot := range o.Metadata {
		metadataNames[i] = o.Name + slot
		requests = append(requests, request{&metadataNames[i], true})
	}
	symbols := func(name string, metadata bool) []string {
		result := []string{name}
		if metadata {
			public, cached := jsonDescriptorNames(name)
			fields, set, cachedFields := validationDescriptorNames(name)
			result = append(result, public, cached, fields, set, cachedFields)
		}
		return result
	}
	keys := func(name string, metadata bool) []string {
		result := symbols(name, metadata)
		if metadata {
			// DTOs also own a generated filename: acronym spellings can share it.
			result = append(result, "file:"+snake(name))
		}
		return result
	}
	reserved, used := make(map[string]bool), make(map[string]bool)
	for _, r := range requests {
		for _, symbol := range symbols(*r.name, r.metadata) {
			if !validScaffoldIdentifier(symbol) {
				return scaffoldModelNames{}, fmt.Errorf("model scaffold derives invalid Go identifier %q", symbol)
			}
		}
		for _, symbol := range keys(*r.name, r.metadata) {
			reserved[symbol] = true
		}
	}
	for _, r := range requests {
		base := *r.name
		for suffix := 0; ; suffix++ {
			candidate := base
			if suffix != 0 {
				candidate += strconv.Itoa(suffix)
			}
			available := true
			for _, symbol := range symbols(candidate, r.metadata) {
				if !validScaffoldIdentifier(symbol) {
					return scaffoldModelNames{}, fmt.Errorf("model scaffold derives invalid Go identifier %q", symbol)
				}
			}
			family := keys(candidate, r.metadata)
			for _, symbol := range family {
				if used[symbol] || suffix != 0 && reserved[symbol] {
					available = false
					break
				}
			}
			if !available {
				continue
			}
			*r.name = candidate
			for _, symbol := range family {
				used[symbol] = true
			}
			break
		}
	}
	for i, slot := range o.Metadata {
		n.metadata[slot] = metadataNames[i]
	}
	return n, nil
}

func validScaffoldIdentifier(name string) bool {
	return name != "_" && token.IsIdentifier(name) && types.Universe.Lookup(name) == nil
}
