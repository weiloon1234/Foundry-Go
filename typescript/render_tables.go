package typescript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/enum"
)

func (r *renderer) tables() {
	if len(r.document.Tables) == 0 {
		return
	}

	tableIDs := make([]string, len(r.document.Tables))
	for i, t := range r.document.Tables {
		tableIDs[i] = quote(string(t.ID))
	}
	fmt.Fprintf(&r.out, "\nexport type TableId = %s;\n\n", strings.Join(tableIDs, " | "))

	r.out.WriteString("export type TableRow<Id extends TableId> =\n")
	for _, t := range r.document.Tables {
		fmt.Fprintf(&r.out, "  Id extends %s ? %s :\n", quote(string(t.ID)), r.typeName(t.Row))
	}
	r.out.WriteString("  never;\n\n")

	r.out.WriteString("export type TableReceivedRow<Id extends TableId> =\n")
	for _, t := range r.document.Tables {
		fmt.Fprintf(&r.out, "  Id extends %s ? %s :\n", quote(string(t.ID)), r.receivedName(t.Row))
	}
	r.out.WriteString("  never;\n\n")

	r.out.WriteString("export const tables = /* @__PURE__ */ Object.freeze({\n")
	for _, t := range r.document.Tables {
		queryOp, exportOp := r.tableOperations(t)
		rowType := r.typeName(t.Row)
		r.renderTableEntry(t, queryOp, exportOp, rowType)
	}
	r.out.WriteString("} as const);\n")
}

func (r *renderer) tableOperations(table manifest.Table) (queryOp string, exportOp string) {
	tableIDStr := string(table.ID)
	pascalTable := ""
	for _, part := range strings.FieldsFunc(tableIDStr, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		if part != "" {
			pascalTable += strings.ToUpper(part[:1]) + part[1:]
		}
	}

	for _, op := range r.document.HTTP {
		routeID := string(op.Route.ID)
		if queryOp == "" {
			if routeID == tableIDStr+".query" || routeID == tableIDStr+":query" || op.Name == clientNameOrEmpty(tableIDStr+".query") || strings.HasSuffix(op.Name, pascalTable+"Query") {
				queryOp = op.Name
			}
		}
		if exportOp == "" {
			if routeID == tableIDStr+".export" || routeID == tableIDStr+".csv" || routeID == tableIDStr+":export" || routeID == tableIDStr+":csv" ||
				op.Name == clientNameOrEmpty(tableIDStr+".export") || op.Name == clientNameOrEmpty(tableIDStr+".csv") ||
				strings.HasSuffix(op.Name, pascalTable+"Export") || strings.HasSuffix(op.Name, pascalTable+"Csv") {
				exportOp = op.Name
			}
		}
	}
	if queryOp == "" {
		for _, op := range r.document.HTTP {
			if op.Body != nil && op.Body.Type == table.Request {
				queryOp = op.Name
				break
			}
		}
	}
	return queryOp, exportOp
}

func clientNameOrEmpty(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	if len(parts) == 0 {
		return ""
	}
	name := parts[0]
	for _, part := range parts[1:] {
		name += strings.ToUpper(part[:1]) + part[1:]
	}
	return name
}

func (r *renderer) renderTableEntry(t manifest.Table, queryOp, exportOp, rowType string) {
	typeArgs := rowType
	if queryOp != "" {
		typeArgs += ", " + quote(queryOp)
		if exportOp != "" {
			typeArgs += ", " + quote(exportOp)
		}
	}
	fmt.Fprintf(&r.out, "  %s: createTableDescriptor<%s>({\n", quote(string(t.ID)), typeArgs)
	fmt.Fprintf(&r.out, "    id: %s,\n", quote(string(t.ID)))
	if queryOp != "" {
		fmt.Fprintf(&r.out, "    queryOperation: %s,\n", quote(queryOp))
	}
	if exportOp != "" {
		fmt.Fprintf(&r.out, "    exportOperation: %s,\n", quote(exportOp))
	}
	r.out.WriteString("    columns: [\n")
	for _, c := range t.Columns {
		r.renderTableColumn(c)
	}
	r.out.WriteString("    ],\n")
	r.out.WriteString("    defaultSort: [\n")
	for _, s := range t.DefaultSort {
		fmt.Fprintf(&r.out, "      { column: %s, direction: %s },\n", quote(s.Column), quote(string(s.Direction)))
	}
	r.out.WriteString("    ],\n")
	if t.Exports {
		r.out.WriteString("    exports: true,\n")
	} else {
		r.out.WriteString("    exports: false,\n")
	}
	if queryOp != "" {
		fmt.Fprintf(&r.out, "    query(client: API, input: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]> {\n", quote(queryOp), quote(queryOp))
		fmt.Fprintf(&r.out, "      return client[%s](input, options);\n", quote(queryOp))
		r.out.WriteString("    },\n")
	}
	if exportOp != "" {
		fmt.Fprintf(&r.out, "    export(client: API, input: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]> {\n", quote(exportOp), quote(exportOp))
		fmt.Fprintf(&r.out, "      return client[%s](input, options);\n", quote(exportOp))
		r.out.WriteString("    },\n")
	}
	r.out.WriteString("  }),\n")
}

func (r *renderer) renderTableColumn(c datatable.ColumnInfo) {
	kind := string(c.Kind)
	if kind == "" {
		kind = string(columnKindFromType(c.Value.Value))
	}
	fmt.Fprintf(&r.out, "      {\n")
	fmt.Fprintf(&r.out, "        name: %s,\n", quote(c.Name))
	fmt.Fprintf(&r.out, "        label: %s,\n", quote(string(c.Label)))
	fmt.Fprintf(&r.out, "        kind: %s,\n", quote(kind))
	if c.Value.Value.Nullable {
		r.out.WriteString("        nullable: true,\n")
	} else {
		r.out.WriteString("        nullable: false,\n")
	}
	if c.Sortable {
		r.out.WriteString("        sortable: true,\n")
	} else {
		r.out.WriteString("        sortable: false,\n")
	}
	if c.Searchable {
		r.out.WriteString("        searchable: true,\n")
	} else {
		r.out.WriteString("        searchable: false,\n")
	}
	if c.Exportable {
		r.out.WriteString("        exportable: true,\n")
	} else {
		r.out.WriteString("        exportable: false,\n")
	}
	if c.Filter != nil && len(c.Filter.Operators) > 0 {
		ops := make([]string, len(c.Filter.Operators))
		for i, op := range c.Filter.Operators {
			ops[i] = quote(string(op))
		}
		fmt.Fprintf(&r.out, "        operators: [%s],\n", strings.Join(ops, ", "))
	} else {
		r.out.WriteString("        operators: [],\n")
	}
	if kind == string(datatable.ColumnEnum) {
		def := r.findEnumDefinition(c.Value.Value.ID)
		if def != nil {
			enumTypeName := r.typeName(c.Value.Value.ID)
			fmt.Fprintf(&r.out, "        enum: %s,\n", enumTypeName)
		}
		r.out.WriteString("        cases: [\n")
		for _, caseRaw := range c.Value.Value.Cases {
			valStr := string(caseRaw)
			var valUnquoted string
			if c.Value.Value.Kind == contract.StringKind {
				_ = json.Unmarshal(caseRaw, &valUnquoted)
			} else {
				valUnquoted = valStr
			}
			labelKey := ""
			if def != nil {
				for _, dCase := range def.Cases {
					var dVal string
					if c.Value.Value.Kind == contract.StringKind {
						_ = json.Unmarshal(dCase.Value, &dVal)
					} else {
						dVal = string(dCase.Value)
					}
					if dVal == valUnquoted {
						labelKey = string(dCase.LabelKey)
						break
					}
				}
			}
			if labelKey != "" {
				fmt.Fprintf(&r.out, "          { value: %s, labelKey: %s },\n", quote(valUnquoted), quote(labelKey))
			} else {
				fmt.Fprintf(&r.out, "          { value: %s },\n", quote(valUnquoted))
			}
		}
		r.out.WriteString("        ],\n")
	}
	fmt.Fprintf(&r.out, "      },\n")
}

func columnKindFromType(val contract.Type) datatable.ColumnKind {
	if len(val.Cases) > 0 {
		return datatable.ColumnEnum
	}
	switch val.Kind {
	case contract.BooleanKind:
		return datatable.ColumnBoolean
	case contract.IntegerKind:
		return datatable.ColumnInteger
	case contract.NumberKind:
		return datatable.ColumnDecimal
	}
	switch val.Format {
	case contract.DecimalFormat:
		return datatable.ColumnDecimal
	case contract.DateFormat:
		return datatable.ColumnDate
	case contract.DateTimeFormat:
		return datatable.ColumnDateTime
	case contract.LocalDateTimeFormat:
		return datatable.ColumnLocalDateTime
	case contract.TimeFormat:
		return datatable.ColumnTime
	default:
		return datatable.ColumnText
	}
}

func (r *renderer) findEnumDefinition(id contract.TypeID) *enum.Definition {
	idStr := string(id)
	dot := strings.LastIndexByte(idStr, '.')
	if dot < 0 {
		return nil
	}
	pkg := idStr[:dot]
	name := idStr[dot+1:]
	for i := range r.document.Enums {
		def := &r.document.Enums[i]
		if def.Name == name && (def.PackagePath == pkg || strings.HasSuffix(pkg, def.PackagePath)) {
			return def
		}
	}
	for i := range r.document.Enums {
		def := &r.document.Enums[i]
		if def.Name == name {
			return def
		}
	}
	return nil
}
